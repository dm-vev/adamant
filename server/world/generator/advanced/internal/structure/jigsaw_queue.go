package structure

import "math"

type jigsawPending struct {
	piece, depth int
	space        *jigsawSpace
}

// jigsawQueue follows SequencedPriorityIterator: highest priority first, FIFO
// within a priority. Buckets use rings so long chains do not allocate per pop.
type jigsawQueue struct {
	buckets  map[int32]*jigsawBucket
	current  *jigsawBucket
	priority int32
}
type jigsawBucket struct {
	values     []jigsawPending
	head, size int
}

func (q *jigsawQueue) push(value jigsawPending, priority int32) {
	bucket := q.current
	if bucket == nil || priority != q.priority {
		if q.buckets == nil {
			q.buckets = make(map[int32]*jigsawBucket)
		}
		bucket = q.buckets[priority]
		if bucket == nil {
			bucket = &jigsawBucket{}
			q.buckets[priority] = bucket
		}
		if q.current == nil || priority >= q.priority {
			q.current, q.priority = bucket, priority
		}
	}
	if bucket.size == len(bucket.values) {
		grown := make([]jigsawPending, max(4, len(bucket.values)*2))
		for i := range bucket.size {
			grown[i] = bucket.values[(bucket.head+i)%len(bucket.values)]
		}
		bucket.values, bucket.head = grown, 0
	}
	index := bucket.head + bucket.size
	if index >= len(bucket.values) {
		index -= len(bucket.values)
	}
	bucket.values[index] = value
	bucket.size++
}
func (q *jigsawQueue) pop() (jigsawPending, bool) {
	bucket := q.current
	if bucket == nil {
		return jigsawPending{}, false
	}
	value := bucket.values[bucket.head]
	bucket.values[bucket.head] = jigsawPending{}
	bucket.head++
	if bucket.head == len(bucket.values) {
		bucket.head = 0
	}
	bucket.size--
	if bucket.size == 0 {
		q.current, q.priority = nil, math.MinInt32
		for priority, candidate := range q.buckets {
			if priority > q.priority && candidate.size > 0 {
				q.current, q.priority = candidate, priority
			}
		}
	}
	return value, true
}
