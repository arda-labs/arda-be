package model

import "strings"

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

// thinkFilter splits inline <think>…</think> reasoning out of streamed content.
// Some open-weight reasoning models (DeepSeek-R1 distills, QwQ, MiniMax) served
// through vLLM or Ollama return their chain of thought inside the content
// instead of a separate reasoning field, which would otherwise be shown to the
// user as part of the answer and replayed into later turns.
//
// Only a block that opens the turn is treated as reasoning, so an answer that
// merely mentions the tag is not swallowed. Tags may be split across deltas.
type thinkFilter struct {
	buf      strings.Builder
	started  bool // first non-space content seen
	inThink  bool
	finished bool // the leading think block is closed; everything is text
}

// feed consumes one content delta and reports the text and reasoning it
// resolves to. Text that could still be the start of a tag is held back.
func (f *thinkFilter) feed(delta string) (text, reasoning string) {
	if f.finished {
		return delta, ""
	}
	f.buf.WriteString(delta)
	return f.drain(false)
}

// flush releases anything still held back at the end of the stream.
func (f *thinkFilter) flush() (text, reasoning string) {
	if f.finished {
		return "", ""
	}
	return f.drain(true)
}

func (f *thinkFilter) drain(final bool) (text, reasoning string) {
	var outText, outReasoning strings.Builder
	for {
		pending := f.buf.String()
		if pending == "" {
			break
		}
		if !f.started {
			trimmed := strings.TrimLeft(pending, " \t\r\n")
			if trimmed == "" {
				if final {
					outText.WriteString(pending)
					f.buf.Reset()
				}
				break
			}
			if strings.HasPrefix(trimmed, thinkOpen) {
				f.started, f.inThink = true, true
				f.buf.Reset()
				f.buf.WriteString(trimmed[len(thinkOpen):])
				continue
			}
			if !final && strings.HasPrefix(thinkOpen, trimmed) {
				break // might still become <think>
			}
			f.started, f.finished = true, true
			outText.WriteString(pending)
			f.buf.Reset()
			break
		}
		if f.inThink {
			if index := strings.Index(pending, thinkClose); index >= 0 {
				outReasoning.WriteString(pending[:index])
				rest := strings.TrimLeft(pending[index+len(thinkClose):], "\r\n")
				f.buf.Reset()
				f.inThink, f.finished = false, true
				outText.WriteString(rest)
				break
			}
			keep := 0
			if !final {
				keep = partialSuffix(pending, thinkClose)
			}
			outReasoning.WriteString(pending[:len(pending)-keep])
			f.buf.Reset()
			f.buf.WriteString(pending[len(pending)-keep:])
			break
		}
		outText.WriteString(pending)
		f.buf.Reset()
		break
	}
	return outText.String(), outReasoning.String()
}

// partialSuffix returns the length of the longest suffix of s that is a proper
// prefix of tag.
func partialSuffix(s, tag string) int {
	max := len(tag) - 1
	if max > len(s) {
		max = len(s)
	}
	for n := max; n > 0; n-- {
		if strings.HasSuffix(s, tag[:n]) {
			return n
		}
	}
	return 0
}
