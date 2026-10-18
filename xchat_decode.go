package x

// Field layout derived from X Corp.'s MIT-licensed event.thrift/product.thrift
// at 4f06a02f4fbfe7016ffe93674fd89a74a2603047. See XCHAT_SCHEMA_LICENSE.
import (
	"encoding/base64"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"
)

type chatDecodedEvent struct {
	SequenceID, MessageID, SenderID, ConversationID, PreviousSequenceID string
	CreatedAt                                                           time.Time
	Kind, Text                                                          string
	Encrypted, SignaturePresent, Attachments                            bool
}

var errChatDecode = errors.New("x: invalid chat event")

const chatMaxBytes = 1 << 20

// Parsing has independent byte, nesting, container and total-value bounds.
type chatWire struct {
	data        []byte
	pos, values int
}
type chatValue struct {
	typ          byte
	element, key byte
	bytes        []byte
	fields       map[int16]chatValue
	count        int
}

func (r *chatWire) take(n int) ([]byte, error) {
	if n < 0 || n > len(r.data)-r.pos {
		return nil, errChatDecode
	}
	v := r.data[r.pos : r.pos+n]
	r.pos += n
	return v, nil
}
func (r *chatWire) number(n int) (uint64, error) {
	b, e := r.take(n)
	if e != nil {
		return 0, e
	}
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v, nil
}
func (r *chatWire) value(t byte, depth int) (chatValue, error) {
	v := chatValue{typ: t}
	r.values++
	if depth > 16 || r.values > 8192 {
		return v, errChatDecode
	}
	switch t {
	case 2, 3:
		b, e := r.take(1)
		if e != nil || t == 2 && b[0] > 1 {
			return v, errChatDecode
		}
	case 4, 10:
		if _, e := r.take(8); e != nil {
			return v, e
		}
	case 6:
		if _, e := r.take(2); e != nil {
			return v, e
		}
	case 8:
		if _, e := r.take(4); e != nil {
			return v, e
		}
	case 11:
		n, e := r.number(4)
		if e != nil || n > chatMaxBytes {
			return v, errChatDecode
		}
		v.bytes, e = r.take(int(n))
		if e != nil {
			return v, e
		}
	case 12:
		v.fields = map[int16]chatValue{}
		for {
			t, e := r.number(1)
			if e != nil {
				return v, e
			}
			if t == 0 {
				break
			}
			id, e := r.number(2)
			if e != nil {
				return v, e
			}
			if len(v.fields) >= 256 {
				return v, errChatDecode
			}
			if _, ok := v.fields[int16(id)]; ok {
				return v, errChatDecode
			}
			child, e := r.value(byte(t), depth+1)
			if e != nil {
				return v, e
			}
			v.fields[int16(id)] = child
		}
	case 13, 14, 15:
		key, e := r.number(1)
		if e != nil {
			return v, e
		}
		element := key
		if t == 13 {
			element, e = r.number(1)
			if e != nil {
				return v, e
			}
		}
		n, e := r.number(4)
		if e != nil || n > 1024 {
			return v, errChatDecode
		}
		if !chatWireType(byte(element)) || t == 13 && !chatWireType(byte(key)) {
			return v, errChatDecode
		}
		v.count = int(n)
		v.element, v.key = byte(element), byte(key)
		for i := 0; i < int(n); i++ {
			if t == 13 {
				if _, e = r.value(byte(key), depth+1); e != nil {
					return v, e
				}
			}
			if _, e = r.value(byte(element), depth+1); e != nil {
				return v, e
			}
		}
	default:
		return v, errChatDecode
	}
	return v, nil
}
func chatWireType(t byte) bool {
	switch t {
	case 2, 3, 4, 6, 8, 10, 11, 12, 13, 14, 15:
		return true
	}
	return false
}
func chatMessageID(s string) bool {
	if dmNumericID(s) {
		return true
	}
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
func chatFields(v chatValue, types map[int16]byte) error {
	for id, t := range types {
		if f, ok := v.fields[id]; ok && f.typ != t {
			return errChatDecode
		}
	}
	return nil
}
func chatList(v chatValue, element byte) bool {
	return v.typ == 0 || v.typ == 15 && v.element == element
}
func chatString(v chatValue) string { return string(v.bytes) }
func decodeChatStruct(data []byte) (chatValue, error) {
	r := chatWire{data: data}
	v, e := r.value(12, 0)
	if e != nil || r.pos != len(data) {
		return v, errChatDecode
	}
	return v, nil
}
func decodeChatEvent(encoded string) (chatDecodedEvent, error) {
	var out chatDecodedEvent
	if len(encoded) > base64.StdEncoding.EncodedLen(chatMaxBytes) {
		return out, errChatDecode
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(encoded)
	if e != nil || len(raw) > chatMaxBytes {
		return out, errChatDecode
	}
	event, e := decodeChatStruct(raw)
	if e != nil {
		return out, e
	}
	if e = chatFields(event, map[int16]byte{1: 11, 2: 11, 3: 11, 4: 11, 5: 11, 6: 11, 7: 12, 8: 8, 9: 12, 10: 11, 11: 2}); e != nil {
		return out, e
	}
	out.SequenceID = chatString(event.fields[1])
	out.MessageID = chatString(event.fields[2])
	out.SenderID = chatString(event.fields[3])
	out.ConversationID = chatString(event.fields[4])
	out.PreviousSequenceID = chatString(event.fields[10])
	if !dmNumericID(out.SequenceID) || !chatMessageID(out.MessageID) || !dmNumericID(out.SenderID) || out.ConversationID == "" || len(out.ConversationID) > 256 || !utf8.ValidString(out.ConversationID) || out.PreviousSequenceID != "" && !dmNumericID(out.PreviousSequenceID) {
		return chatDecodedEvent{}, errChatDecode
	}
	ms, e := strconv.ParseInt(chatString(event.fields[6]), 10, 64)
	if e != nil || ms <= 0 {
		return chatDecodedEvent{}, errChatDecode
	}
	out.CreatedAt = time.UnixMilli(ms)
	if sig, ok := event.fields[9]; ok {
		if e = chatFields(sig, map[int16]byte{1: 11, 2: 11, 3: 11, 4: 11, 5: 15}); e != nil {
			return chatDecodedEvent{}, e
		}
		if !chatList(sig.fields[5], 12) {
			return chatDecodedEvent{}, errChatDecode
		}
		out.SignaturePresent = len(sig.fields[1].bytes) > 0
	}
	detail, ok := event.fields[7]
	if !ok || len(detail.fields) != 1 {
		return chatDecodedEvent{}, errChatDecode
	}
	out.Kind = "other_event"
	for id, body := range detail.fields {
		if body.typ != 12 {
			return chatDecodedEvent{}, errChatDecode
		}
		if id != 1 {
			switch id {
			case 3:
				out.Kind = "key_change"
			case 6:
				out.Kind = "typing"
			case 12, 13:
				out.Kind = "read_notice"
			}
			return out, nil
		}
		if e = chatFields(body, map[int16]byte{100: 11, 101: 11, 102: 2, 103: 10, 104: 10, 105: 2, 106: 8, 107: 15, 108: 12, 109: 2}); e != nil {
			return chatDecodedEvent{}, e
		}
		if !chatList(body.fields[107], 8) {
			return chatDecodedEvent{}, errChatDecode
		}
		contents, ok := body.fields[100]
		if !ok || len(contents.bytes) == 0 {
			return chatDecodedEvent{}, errChatDecode
		}
		if _, exists := body.fields[101]; exists {
			out.Encrypted = true
			out.Kind = "human"
			return out, nil
		}
		holder, e := decodeChatStruct(contents.bytes)
		if e != nil {
			return chatDecodedEvent{}, e
		}
		if e = chatFields(holder, map[int16]byte{1: 12}); e != nil {
			return chatDecodedEvent{}, e
		}
		union, ok := holder.fields[1]
		if !ok || len(union.fields) != 1 {
			return chatDecodedEvent{}, errChatDecode
		}
		for contentID, content := range union.fields {
			if content.typ != 12 {
				return chatDecodedEvent{}, errChatDecode
			}
			if contentID != 1 {
				return out, nil
			}
			if e = chatFields(content, map[int16]byte{1: 11, 2: 15, 3: 15, 4: 12, 6: 12, 7: 8, 8: 12, 9: 15, 10: 13}); e != nil {
				return chatDecodedEvent{}, e
			}
			if !chatList(content.fields[2], 12) || !chatList(content.fields[3], 12) || !chatList(content.fields[9], 12) {
				return chatDecodedEvent{}, errChatDecode
			}
			if m, ok := content.fields[10]; ok && (m.key != 11 || m.element != 11) {
				return chatDecodedEvent{}, errChatDecode
			}
			text := content.fields[1].bytes
			if len(text) > 65536 || !utf8.Valid(text) {
				return chatDecodedEvent{}, errChatDecode
			}
			out.Kind = "human"
			out.Text = string(text)
			out.Attachments = content.fields[3].count > 0
		}
	}
	return out, nil
}
