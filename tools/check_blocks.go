package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/streamingfast/bstream"
	"github.com/streamingfast/bstream/forkable"
	"github.com/streamingfast/dstore"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

var numberRegex = regexp.MustCompile(`(\d{10})`)

type PrintDetails uint8

const (
	PrintNoDetails PrintDetails = iota
	PrintStats
	PrintFull
	MaxUint64 = ^uint64(0)
)

func CheckMergedBlocks(
	ctx context.Context,
	logger *zap.Logger,
	storeURL string,
	fileBlockSize uint64,
	blockRange BlockRange,
	blockPrinter func(block *bstream.Block),
	printDetails PrintDetails,
) error {
	readAllBlocks := printDetails != PrintNoDetails
	fmt.Printf("Checking block holes on %s\n", storeURL)
	if readAllBlocks {
		fmt.Println("Detailed printing requested: All block files will be read and checked for continuity. This may take a while...")
	}

	var expected uint64
	var count int
	var highestBlockSeen uint64
	lowestBlockSeen := MaxUint64

	if !blockRange.IsResolved() {
		return fmt.Errorf("check merged blocks can only work with fully resolved range, got %s", blockRange)
	}

	// if blockRange.Start < bstream.GetProtocolFirstStreamableBlock {
	// 	blockRange.Start = bstream.GetProtocolFirstStreamableBlock
	// }

	holeFound := false
	expected = RoundToBundleStartBlock(uint64(blockRange.Start), fileBlockSize)
	currentStartBlk := uint64(blockRange.Start)

	blocksStore, err := dstore.NewDBinStore(storeURL)
	if err != nil {
		return err
	}

	walkPrefix := WalkBlockPrefix(blockRange, fileBlockSize)

	tfdb := &trackedForkDB{
		fdb: forkable.NewForkDB(),
	}

	logger.Debug("walking merged blocks", zap.Stringer("block_range", blockRange), zap.String("walk_prefix", walkPrefix))
	err = blocksStore.Walk(ctx, walkPrefix, func(filename string) error {
		match := numberRegex.FindStringSubmatch(filename)
		if match == nil {
			return nil
		}

		logger.Debug("received merged blocks", zap.String("filename", filename))

		count++
		baseNum, _ := strconv.ParseUint(match[1], 10, 32)
		if baseNum+uint64(fileBlockSize)-1 < uint64(blockRange.Start) {
			logger.Debug("base num lower then block range start, quitting", zap.Uint64("base_num", baseNum), zap.Int64("starting_at", blockRange.Start))
			return nil
		}

		if baseNum != expected {
			// There is no previous valid block range if we are at the ever first seen file
			if count > 1 {
				fmt.Printf("✅ Range %s\n", NewClosedRange(int64(currentStartBlk), uint64(RoundToBundleEndBlock(expected-fileBlockSize, fileBlockSize))))
			}

			// Otherwise, we do not follow last seen element (previous is `100 - 199` but we are `299 - 300`)
			missingRange := NewClosedRange(int64(expected), RoundToBundleEndBlock(baseNum-fileBlockSize, fileBlockSize))
			fmt.Printf("❌ Range %s (Missing, [%s])\n", missingRange, missingRange.ReprocRange())
			currentStartBlk = baseNum

			holeFound = true
		}
		expected = baseNum + fileBlockSize

		if readAllBlocks {
			lowestBlockSegment, highestBlockSegment := validateBlockSegment(ctx, blocksStore, filename, fileBlockSize, blockRange, blockPrinter, printDetails, tfdb)
			if lowestBlockSegment < lowestBlockSeen {
				lowestBlockSeen = lowestBlockSegment
			}
			if highestBlockSegment > highestBlockSeen {
				highestBlockSeen = highestBlockSegment
			}
		} else {
			if baseNum < lowestBlockSeen {
				lowestBlockSeen = baseNum
			}
			if baseNum+fileBlockSize > highestBlockSeen {
				highestBlockSeen = baseNum + fileBlockSize
			}
		}

		if count%10000 == 0 {
			fmt.Printf("✅ Range %s\n", NewClosedRange(int64(currentStartBlk), RoundToBundleEndBlock(baseNum, fileBlockSize)))
			currentStartBlk = baseNum + fileBlockSize
		}

		if blockRange.IsClosed() && RoundToBundleEndBlock(baseNum, fileBlockSize) >= *blockRange.Stop-1 {
			return dstore.StopIteration
		}

		return nil
	})

	if err != nil {
		return err
	}

	logger.Debug("checking incomplete range",
		zap.Stringer("range", blockRange),
		zap.Bool("range_unbounded", blockRange.IsOpen()),
		zap.Uint64("lowest_block_seen", lowestBlockSeen),
		zap.Uint64("highest_block_seen", highestBlockSeen),
	)
	if tfdb.lastLinkedBlock != nil && tfdb.lastLinkedBlock.Number < highestBlockSeen {
		fmt.Printf("🔶 Range %s has issues with forks, last linkable block number: %d\n", NewClosedRange(int64(currentStartBlk), uint64(highestBlockSeen)), tfdb.lastLinkedBlock.Number)
	} else {
		fmt.Printf("✅ Range %s\n", NewClosedRange(int64(currentStartBlk), uint64(highestBlockSeen)))
	}

	fmt.Println()
	fmt.Println("Summary:")

	if blockRange.IsClosed() &&
		(highestBlockSeen < uint64(*blockRange.Stop-1) ||
			(lowestBlockSeen > uint64(blockRange.Start) && lowestBlockSeen > bstream.GetProtocolFirstStreamableBlock)) {
		fmt.Printf("> 🔶 Incomplete range %s, started at block %s and stopped at block: %s\n", blockRange, PrettyBlockNum(lowestBlockSeen), PrettyBlockNum(highestBlockSeen))
	}

	if holeFound {
		fmt.Printf("> 🆘 Holes found!\n")
	} else {
		fmt.Printf("> 🆗 No hole found\n")
	}

	return nil
}

type trackedForkDB struct {
	fdb                    *forkable.ForkDB
	firstUnlinkableBlock   *bstream.Block
	lastLinkedBlock        *bstream.Block
	unlinkableSegmentCount int
}

func validateBlockSegment(
	ctx context.Context,
	store dstore.Store,
	segment string,
	fileBlockSize uint64,
	blockRange BlockRange,
	blockPrinter func(block *bstream.Block),
	printDetails PrintDetails,
	tfdb *trackedForkDB,
) (lowestBlockSeen, highestBlockSeen uint64) {
	lowestBlockSeen = MaxUint64
	reader, err := store.OpenObject(ctx, segment)
	if err != nil {
		fmt.Printf("❌ Unable to read blocks segment %s: %s\n", segment, err)
		return
	}
	defer reader.Close()

	readerFactory, err := bstream.GetBlockReaderFactory.New(reader)
	if err != nil {
		fmt.Printf("❌ Unable to read blocks segment %s: %s\n", segment, err)
		return
	}

	seenBlockCount := 0
	for {
		block, err := readerFactory.Read()
		if block != nil {
			if block.Number < uint64(blockRange.Start) {
				continue
			}

			if blockRange.IsClosed() && block.Number > uint64(*blockRange.Stop) {
				return
			}

			if block.Number < lowestBlockSeen {
				lowestBlockSeen = block.Number
			}
			if block.Number > highestBlockSeen {
				highestBlockSeen = block.Number
			}

			if !tfdb.fdb.HasLIB() {
				tfdb.fdb.InitLIB(block)
			}

			tfdb.fdb.AddLink(block.AsRef(), block.PreviousID(), nil)
			revSeg, _ := tfdb.fdb.ReversibleSegment(block.AsRef())
			if revSeg == nil {
				tfdb.unlinkableSegmentCount++
				if tfdb.firstUnlinkableBlock == nil {
					tfdb.firstUnlinkableBlock = block
				}

				// TODO: this print should be under a 'check forkable' flag?
				fmt.Printf("🔶 Block #%d is not linkable at this point\n", block.Num())

				if tfdb.unlinkableSegmentCount > 99 && tfdb.unlinkableSegmentCount%100 == 0 {
					// TODO: this print should be under a 'check forkable' flag?
					fmt.Printf("❌ Large gap of %d unlinkable blocks found in chain. Last linked block: %d, first Unlinkable block: %d. \n", tfdb.unlinkableSegmentCount, tfdb.lastLinkedBlock.Num(), tfdb.firstUnlinkableBlock.Num())
				}
			} else {
				tfdb.lastLinkedBlock = block
				tfdb.unlinkableSegmentCount = 0
				tfdb.firstUnlinkableBlock = nil
				tfdb.fdb.SetLIB(block, block.PreviousId, block.LibNum)
				if tfdb.fdb.HasLIB() {
					tfdb.fdb.PurgeBeforeLIB(0)
				}
			}
			seenBlockCount++

			if printDetails == PrintStats {
				blockPrinter(block)
			}

			if printDetails == PrintFull {
				out, err := json.MarshalIndent(block.ToProtocol().(proto.Message), "", "  ")
				if err != nil {
					fmt.Printf("❌ Unable to print full block %s: %s\n", block.AsRef(), err)
					continue
				}

				fmt.Println(string(out))
			}

			continue
		}

		if block == nil && err == io.EOF {
			if seenBlockCount < expectedBlockCount(segment, fileBlockSize) {
				fmt.Printf("🔶 Segment %s contained only %d blocks (< 100), this can happen on some chains\n", segment, seenBlockCount)
			}

			return
		}

		if err != nil {
			fmt.Printf("❌ Unable to read all blocks from segment %s after reading %d blocks: %s\n", segment, seenBlockCount, err)
			return
		}
	}
}

func WalkBlockPrefix(blockRange BlockRange, fileBlockSize uint64) string {
	if blockRange.IsOpen() {
		return ""
	}

	startString := fmt.Sprintf("%010d", RoundToBundleStartBlock(uint64(blockRange.Start), fileBlockSize))
	endString := fmt.Sprintf("%010d", RoundToBundleEndBlock(uint64(*blockRange.Stop-1), fileBlockSize)+1)

	offset := 0
	for i := 0; i < len(startString); i++ {
		if startString[i] != endString[i] {
			return string(startString[0:i])
		}

		offset++
	}

	// At this point, the two strings are equal, to return the string
	return startString
}

func expectedBlockCount(segment string, fileBlockSize uint64) int {
	if segment == "0000000000" {
		return int(fileBlockSize - bstream.GetProtocolFirstStreamableBlock)
	}

	return int(fileBlockSize)
}
