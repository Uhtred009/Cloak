package multiplex

// The data is multiplexed through several TCP connections, therefore the
// order of arrival is not guaranteed. A stream's first packet may be sent through
// connection0 and its second packet may be sent through connection1. Although both
// packets are transmitted reliably (as TCP is reliable), packet1 may arrive to the
// remote side before packet0. Cloak have to therefore sequence the packets so that they
// arrive in order as they were sent by the proxy software
//
// Cloak packets will have a 32-bit sequence number on them, so we know in which order
// they should be sent to the proxy software. The code in this file provides buffering and sorting.

import (
	"container/heap"
	"fmt"
	"sync"
)

type sorterHeap []*Frame

func (sh sorterHeap) Less(i, j int) bool {
	return sh[i].Seq < sh[j].Seq
}
func (sh sorterHeap) Len() int {
	return len(sh)
}
func (sh sorterHeap) Swap(i, j int) {
	sh[i], sh[j] = sh[j], sh[i]
}

func (sh *sorterHeap) Push(x interface{}) {
	*sh = append(*sh, x.(*Frame))
}

func (sh *sorterHeap) Pop() interface{} {
	old := *sh
	n := len(old)
	x := old[n-1]
	*sh = old[0 : n-1]
	return x
}

type streamBuffer struct {
	recvM sync.Mutex

	nextRecvSeq uint64
	rev         int
	sh          sorterHeap

	buf *bufferedPipe
}

func NewStreamBuffer() *streamBuffer {
	sb := &streamBuffer{
		sh:  []*Frame{},
		rev: 0,
		buf: NewBufferedPipe(),
	}
	return sb
}

// recvNewFrame is a forever running loop which receives frames unordered,
// cache and order them and send them into sortedBufCh
func (sb *streamBuffer) Write(f Frame) error {
	sb.recvM.Lock()
	defer sb.recvM.Unlock()
	// when there'fs no ooo packages in heap and we receive the next package in order
	if len(sb.sh) == 0 && f.Seq == sb.nextRecvSeq {
		if f.Closing == 1 {
			sb.buf.Close()
			return nil
		} else {
			sb.buf.Write(f.Payload)
			sb.nextRecvSeq += 1
		}
		return nil
	}

	if f.Seq < sb.nextRecvSeq {
		return fmt.Errorf("seq %v is smaller than nextRecvSeq %v", f.Seq, sb.nextRecvSeq)
	}

	heap.Push(&sb.sh, &f)
	// Keep popping from the heap until empty or to the point that the wanted seq was not received
	for len(sb.sh) > 0 && sb.sh[0].Seq == sb.nextRecvSeq {
		f = *heap.Pop(&sb.sh).(*Frame)
		if f.Closing == 1 {
			// empty data indicates closing signal
			sb.buf.Close()
			return nil
		} else {
			sb.buf.Write(f.Payload)
			sb.nextRecvSeq += 1
		}
	}
	return nil
}

func (sb *streamBuffer) Read(buf []byte) (int, error) {
	return sb.buf.Read(buf)
}

func (sb *streamBuffer) Close() error {
	return sb.buf.Close()
}
