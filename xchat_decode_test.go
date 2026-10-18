package x

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
)

// Synthetic Thrift binary vectors use only the pinned official schema field IDs.
func chatTestField(t byte, id int16, value []byte) []byte {
	b := []byte{t, byte(id >> 8), byte(id)}
	return append(b, value...)
}
func chatTestString(s string) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(len(s)))
	return append(b, s...)
}
func chatTestStruct(fields ...[]byte) []byte { return append(bytes.Join(fields, nil), 0) }
func chatTestEvent(detail []byte, extra ...[]byte) string {
	fields := [][]byte{chatTestField(11, 1, chatTestString("1980000000000000011")), chatTestField(11, 2, chatTestString("ab818bd2-304a-48e2-8767-4f889b792403")), chatTestField(11, 3, chatTestString("123")), chatTestField(11, 4, chatTestString("123:456")), chatTestField(11, 6, chatTestString("1792308000000")), chatTestField(12, 7, detail), chatTestField(11, 10, chatTestString("1980000000000000010"))}
	fields = append(fields, extra...)
	return base64.StdEncoding.EncodeToString(chatTestStruct(fields...))
}
func chatTestPlain(text string, contentKind int16, attachments bool) []byte {
	fields := [][]byte{chatTestField(11, 1, chatTestString(text))}
	if attachments {
		fields = append(fields, chatTestField(15, 3, []byte{12, 0, 0, 0, 1, 0}))
	}
	holder := chatTestStruct(chatTestField(12, 1, chatTestStruct(chatTestField(12, contentKind, chatTestStruct(fields...)))))
	return chatTestStruct(chatTestField(12, 1, chatTestStruct(chatTestField(11, 100, chatTestString(string(holder))))))
}
func TestDecodeChatEventPlaintextOfficialSchema(t *testing.T) {
	event, e := decodeChatEvent(chatTestEvent(chatTestPlain("hello", 1, true), chatTestField(12, 9, chatTestStruct(chatTestField(11, 1, chatTestString("unverified-signature"))))))
	if e != nil || event.Kind != "human" || event.Text != "hello" || !event.Attachments || event.Encrypted || !event.SignaturePresent || event.SequenceID == event.MessageID || event.MessageID != "ab818bd2-304a-48e2-8767-4f889b792403" || event.PreviousSequenceID != "1980000000000000010" || event.CreatedAt.UnixMilli() != 1792308000000 {
		t.Fatal(event, e)
	}
}
func TestDecodeChatEventEncryptedNoText(t *testing.T) {
	for _, version := range []string{"", "version1"} {
		body := chatTestStruct(chatTestField(11, 100, chatTestString("not plaintext")), chatTestField(11, 101, chatTestString(version)))
		event, e := decodeChatEvent(chatTestEvent(chatTestStruct(chatTestField(12, 1, body))))
		if e != nil || !event.Encrypted || event.Text != "" || event.Kind != "human" {
			t.Fatal(event, e)
		}
	}
}
func TestDecodeChatEventNonhumanUnions(t *testing.T) {
	for _, id := range []int16{3, 4, 5, 6, 7, 8, 9, 10, 12, 13, 14, 99} {
		event, e := decodeChatEvent(chatTestEvent(chatTestStruct(chatTestField(12, id, chatTestStruct()))))
		if e != nil || event.Kind == "human" || event.Text != "" {
			t.Fatal(id, event, e)
		}
	}
	for _, id := range []int16{2, 3, 4, 10, 11, 16, 99} {
		event, e := decodeChatEvent(chatTestEvent(chatTestPlain("not human", id, false)))
		if e != nil || event.Kind == "human" || event.Text != "" {
			t.Fatal(id, event, e)
		}
	}
}
func TestDecodeChatEventUnknownFieldsAndMalformed(t *testing.T) {
	good := chatTestEvent(chatTestPlain("hello", 1, false))
	if _, e := decodeChatEvent(chatTestEvent(chatTestPlain("hello", 1, false), chatTestField(11, 300, chatTestString("future")))); e != nil {
		t.Fatal(e)
	}
	raw, _ := base64.StdEncoding.DecodeString(good)
	for i := 0; i < len(raw); i++ {
		if _, e := decodeChatEvent(base64.StdEncoding.EncodeToString(raw[:i])); e == nil {
			t.Fatal("accepted truncation", i)
		}
	}
	cases := []string{"!", base64.StdEncoding.EncodeToString(append(raw, 0)), chatTestEvent(chatTestStruct(chatTestField(11, 1, chatTestString("wrong type")))), chatTestEvent(chatTestStruct(chatTestField(12, 1, chatTestStruct()), chatTestField(12, 6, chatTestStruct()))), chatTestEvent(chatTestPlain("ok", 1, false), chatTestField(11, 1, chatTestString("duplicate"))), strings.Repeat("A", base64.StdEncoding.EncodedLen(chatMaxBytes)+4)}
	for i, c := range cases {
		if _, e := decodeChatEvent(c); e == nil {
			t.Fatal("accepted malformed", i)
		}
	}
}
func TestDecodeChatEventLimits(t *testing.T) {
	nested := chatTestStruct()
	for range 18 {
		nested = chatTestStruct(chatTestField(12, 100, nested))
	}
	hugeList := chatTestField(15, 300, []byte{12, 0, 0, 4, 1})
	negativeString := chatTestField(11, 300, []byte{255, 255, 255, 255})
	for _, extra := range [][]byte{chatTestField(12, 300, nested), hugeList, negativeString} {
		if _, e := decodeChatEvent(chatTestEvent(chatTestPlain("ok", 1, false), extra)); e == nil {
			t.Fatal("unbounded input")
		}
	}
	if _, e := decodeChatEvent(chatTestEvent(chatTestPlain(strings.Repeat("x", 65537), 1, false))); e == nil {
		t.Fatal("unbounded text")
	}
}

func TestDecodeChatEventRequiredMetadataAndKnownTypes(t *testing.T) {
	raw, _ := base64.StdEncoding.DecodeString(chatTestEvent(chatTestPlain("ok", 1, false)))
	for _, test := range []struct{ old, new string }{
		{"1980000000000000011", "invalid-sequence!!"},
		{"ab818bd2-304a-48e2-8767-4f889b792403", "zz818bd2-304a-48e2-8767-4f889b792403"},
		{"1792308000000", "0000000000000"},
	} {
		modified := bytes.Replace(raw, []byte(test.old), []byte(test.new), 1)
		if _, err := decodeChatEvent(base64.StdEncoding.EncodeToString(modified)); err == nil {
			t.Fatal("invalid required metadata accepted")
		}
	}
	for _, bad := range [][]byte{
		chatTestStruct(chatTestField(12, 1, chatTestStruct(chatTestField(8, 100, []byte{0, 0, 0, 1})))),
		chatTestStruct(chatTestField(12, 1, chatTestStruct(chatTestField(11, 100, chatTestString(""))))),
	} {
		if _, err := decodeChatEvent(chatTestEvent(bad)); err == nil {
			t.Fatal("invalid known field accepted")
		}
	}
	// Unknown empty containers still require a valid Thrift element type.
	if _, err := decodeChatEvent(chatTestEvent(chatTestPlain("ok", 1, false), chatTestField(15, 300, []byte{99, 0, 0, 0, 0}))); err == nil {
		t.Fatal("invalid empty container type")
	}
}
