// Package bloomindex is a
package bloomindex

// http://bitfunnel.org/strangeloop/

import (
	"errors"
	"github.com/dgryski/go-bits"
)

type DocID uint64

type Index struct {
	blocks []block

	meta []block

	blockSize int
	metaSize  int
	hashes    uint32
	mask      uint32
	mmask     uint32
}

const metaScale = 64

func NewIndex(blockSize, metaSize int, hashes int) *Index {
	idx := &Index{
		blocks:    []block{newBlock(blockSize)},
		meta:      []block{newBlock(metaSize)},
		blockSize: blockSize,
		metaSize:  metaSize,
		hashes:    uint32(hashes),
		mask:      uint32(blockSize) - 1,
		mmask:     uint32(metaSize) - 1,
	}

	// we start out with a single block in our meta index
	idx.meta[0].addDocument()

	return idx
}

func (idx *Index) AddDocument(terms []uint32) DocID {

	blkid := len(idx.blocks) - 1
	if idx.blocks[blkid].numDocuments() == idsPerBlock {
		// full -- allocate a new one
		idx.blocks = append(idx.blocks, newBlock(idx.blockSize))
		blkid++

		mblkid := len(idx.meta) - 1
		if idx.meta[mblkid].numDocuments() == idsPerBlock {
			idx.meta = append(idx.meta, newBlock(idx.metaSize))
			mblkid++
		}
		idx.meta[mblkid].addDocument()

	}
	docid, _ := idx.blocks[blkid].addDocument()

	idx.addTerms(blkid, uint16(docid), terms)

	return DocID(uint64(blkid)*idsPerBlock + uint64(docid))
}

func (idx *Index) addTerms(blockid int, docid uint16, terms []uint32) {

	mblkid := blockid / idsPerBlock
	mid := uint16(blockid % idsPerBlock)

	for _, t := range terms {
		h1, h2 := xorshift32(t), jenkins32(t)
		for i := uint32(0); i < idx.hashes; i++ {
			idx.blocks[blockid].setbit(docid, (h1+i*h2)&idx.mask)
			idx.meta[mblkid].setbit(mid, (h1+i*h2)&idx.mmask)
		}
	}
}

func (idx *Index) Query(terms []uint32) []DocID {

	var docs []DocID

	var bits []uint32
	var mbits []uint32

	for _, t := range terms {
		h1, h2 := xorshift32(t), jenkins32(t)
		for i := uint32(0); i < idx.hashes; i++ {
			bits = append(bits, (h1+i*h2)&idx.mask)
			mbits = append(mbits, (h1+i*h2)&idx.mmask)
		}
	}

	for i, mblk := range idx.meta {

		blks := mblk.query(mbits)

		for _, blkid := range blks {
			b := (i*idsPerBlock + int(blkid))
			blk := idx.blocks[b]

			d := blk.query(bits)
			for _, dd := range d {
				docs = append(docs, DocID(uint64(b*idsPerBlock)+uint64(dd)))
			}

		}
	}

	return docs
}

const idsPerBlock = 512

type bitrow [8]uint64

type block struct {
	bits []bitrow

	// valid is the number of valid documents in this block
	// TODO(dgryski): upgrade to mask at some point
	valid uint64
}

func newBlock(size int) block {
	return block{
		bits: make([]bitrow, size),
	}
}

func (b *block) numDocuments() uint64 {
	return b.valid
}

var errNoSpace = errors.New("block: no space")

func (b *block) addDocument() (uint64, error) {
	if b.valid == idsPerBlock {
		return 0, errNoSpace
	}

	docid := b.valid
	b.valid++
	return docid, nil
}

func (b *block) setbit(docid uint16, bit uint32) {
	b.bits[bit][docid>>6] |= 1 << (docid & 0x3f)
}

func (b *block) getbit(docid uint16, bit uint32) uint64 {
	return b.bits[bit][docid>>6] & (1 << (docid & 0x3f))
}

func (b *block) get(bit uint32) bitrow {
	return b.bits[bit]
}

func (b *block) query(bits []uint32) []uint16 {

	if len(bits) == 0 {
		return nil
	}

	var r bitrow

	queryCore(&r, b.bits, bits)

	// return the IDs of the remaining
	return popset(r)
}

// popset returns which bits are set in r
func popset(b bitrow) []uint16 {
	var r []uint16

	var docid uint64
	for i, u := range b {
		docid = uint64(i) * 64
		for u != 0 {
			tz := bits.Ctz(u)
			u >>= tz + 1
			docid += tz
			r = append(r, uint16(docid))
			docid++
		}
	}

	return r
}

// Xorshift32 is an xorshift RNG
func xorshift32(y uint32) uint32 {

	// http://www.jstatsoft.org/v08/i14/paper
	// Marasaglia's "favourite"

	y ^= (y << 13)
	y ^= (y >> 17)
	y ^= (y << 5)
	return y
}

// jenkins32 is Robert Jenkins' 32-bit integer hash function
func jenkins32(a uint32) uint32 {
	a = (a + 0x7ed55d16) + (a << 12)
	a = (a ^ 0xc761c23c) ^ (a >> 19)
	a = (a + 0x165667b1) + (a << 5)
	a = (a + 0xd3a2646c) ^ (a << 9)
	a = (a + 0xfd7046c5) + (a << 3)
	a = (a ^ 0xb55a4f09) ^ (a >> 16)
	return a
}
