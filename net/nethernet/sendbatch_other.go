//go:build !(linux && (amd64 || arm64))

package nethernet

// sendBatchSupported reports whether a SendBatch sends the datagrams of a
// socket in one call: where it does not, each message goes on its own.
const sendBatchSupported = false

type sysBatch struct{}

func (sb *sockBatch) send() {
	for _, m := range sb.msgs {
		sb.sendOne(m)
	}
}
