package packet

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// QueueStats is a snapshot of the kernel's per-queue statistics exposed
// by /proc/net/netfilter/nfnetlink_queue (one line per bound queue).
//
// The kernel's seq_show format (nfqnl_seq_show in nfnetlink_queue.c):
//
//	%5u %6u %5u %1u %5u %5u %5u %8u %2d
//	queue_num peer_portid queue_total copy_mode copy_range
//	queue_dropped queue_user_dropped id_sequence 1
//
// Interpretation:
//   - Dropped counts packets the kernel failed to deliver to userspace
//     because the queue was full (queue_maxlen reached). The packet is
//     dropped by the kernel — fail-closed by construction (the queue
//     is never configured with NFQA_CFG_F_FAIL_OPEN).
//   - UserDropped counts packets dropped when the netlink unicast to
//     the userspace socket failed (no listener / socket error). Also a
//     kernel drop.
type QueueStats struct {
	QueueNum    uint16
	Total       uint32 // currently queued packets
	Dropped     uint32 // queue full at enqueue time
	UserDropped uint32 // unicast to the peer failed
	Sequence    uint32 // last assigned packet id
}

// ParseLine parses one line of /proc/net/netfilter/nfnetlink_queue.
// It returns ok=false for lines that are not numeric (header, padding)
// or that have the wrong field count.
func ParseLine(line string) (QueueStats, bool) {
	fields := strings.Fields(line)
	if len(fields) != 9 {
		return QueueStats{}, false
	}
	nums := make([]uint64, len(fields))
	for i, f := range fields {
		n, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return QueueStats{}, false
		}
		nums[i] = n
	}
	return QueueStats{
		QueueNum:    uint16(nums[0]),
		Total:       uint32(nums[2]),
		Dropped:     uint32(nums[5]),
		UserDropped: uint32(nums[6]),
		Sequence:    uint32(nums[7]),
	}, true
}

// ParseQueueStats parses the whole proc file content and returns stats
// for the named queue. fileNotFound is true when the file does not
// exist (kernel without /proc/net/netfilter, or module not loaded).
func ParseQueueStats(content string, queue int) (QueueStats, bool, error) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		st, ok := ParseLine(line)
		if !ok {
			continue
		}
		if int(st.QueueNum) == queue {
			return st, true, nil
		}
	}
	return QueueStats{}, false, fmt.Errorf("queue %d not found in nfnetlink_queue stats", queue)
}

// ReadQueueStats reads /proc/net/netfilter/nfnetlink_queue and returns
// the stats for queue. It returns an error when the file is missing or
// unreadable so callers can degrade silently (the kernel does not
// support queue stats).
func ReadQueueStats(path string, queue int) (QueueStats, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return QueueStats{}, fmt.Errorf("read %s: %w", path, err)
	}
	st, found, err := ParseQueueStats(string(data), queue)
	if err != nil {
		return QueueStats{}, err
	}
	if !found {
		return QueueStats{}, fmt.Errorf("queue %d not found in %s", queue, path)
	}
	return st, nil
}
