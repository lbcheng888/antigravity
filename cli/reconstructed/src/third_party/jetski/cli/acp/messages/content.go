// Reconstructed from agy v1.5.10 binary evidence (disassembly + Go type table).
// Binary VA: 0x10025ed00 | Size: 15 funcs (first func Type, 16 B) | Source: third_party/jetski/cli/acp/messages/content.go:L36
// Reconstructed from binary: /Users/mac/.local/bin/agy (v1.5.10)
// Source: third_party/jetski/cli/acp/messages/content.go

package messages

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"
)

// Binary VA: 0x10a161c80 | Size: 16 B
type ContentBlock interface {
	Type() string
	isContentBlock()
}

// Binary VA: 0x10a1d4430 | Size: 16 B
type rawType struct {
	Type string `json:"type"`
}

// Binary VA: 0x10a47e688 | Size: 16 B
type TextContentBlock struct {
	Text string `json:"text"`
}

func (TextContentBlock) isContentBlock() {}

// Binary VA: 0x10025ed00 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/content.go:L36
func (TextContentBlock) Type() string {
	return "text"
}

// Binary VA: 0x10025ed10 | Size: 160 B | Source: third_party/jetski/cli/acp/messages/content.go:L39
func (b TextContentBlock) MarshalJSON() ([]byte, error) {
	type base TextContentBlock
	return jsonv2.Marshal(struct {
		Type string `json:"type"`
		base
	}{
		Type: b.Type(),
		base: base(b),
	})
}

// Binary VA: 0x10a6587d8 | Size: 48 B
type ResourceLinkContentBlock struct {
	URI   string `json:"uri"`
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
}

func (ResourceLinkContentBlock) isContentBlock() {}

// Binary VA: 0x10025edb0 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/content.go:L60
func (ResourceLinkContentBlock) Type() string {
	return "resource_link"
}

// Binary VA: 0x10025edc0 | Size: 208 B | Source: third_party/jetski/cli/acp/messages/content.go:L63
func (b ResourceLinkContentBlock) MarshalJSON() ([]byte, error) {
	type base ResourceLinkContentBlock
	return jsonv2.Marshal(struct {
		Type string `json:"type"`
		base
	}{
		Type: b.Type(),
		base: base(b),
	})
}

// Binary VA: 0x10a658700 | Size: 48 B
type ImageContentBlock struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
	URI      string `json:"uri,omitempty"`
}

func (ImageContentBlock) isContentBlock() {}

// Binary VA: 0x10025ee90 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/content.go:L83
func (ImageContentBlock) Type() string {
	return "image"
}

// Binary VA: 0x10025eea0 | Size: 208 B | Source: third_party/jetski/cli/acp/messages/content.go:L86
func (b ImageContentBlock) MarshalJSON() ([]byte, error) {
	type base ImageContentBlock
	return jsonv2.Marshal(struct {
		Type string `json:"type"`
		base
	}{
		Type: b.Type(),
		base: base(b),
	})
}

// Binary VA: 0x10a60d600 | Size: 64 B
type EmbeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// Binary VA: 0x10025ef70 | Size: 144 B | Source: third_party/jetski/cli/acp/messages/content.go:L106
func (r EmbeddedResource) InlineText() string {
	return "Content of " + r.URI + ":\n" + r.Text
}

// Binary VA: 0x10a47e5e0 | Size: 64 B
type EmbeddedResourceContentBlock struct {
	Resource EmbeddedResource `json:"resource"`
}

func (EmbeddedResourceContentBlock) isContentBlock() {}

// Binary VA: 0x10025f000 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/content.go:L121
func (EmbeddedResourceContentBlock) Type() string {
	return "resource"
}

// Binary VA: 0x10025f010 | Size: 240 B | Source: third_party/jetski/cli/acp/messages/content.go:L124
func (b EmbeddedResourceContentBlock) MarshalJSON() ([]byte, error) {
	type base EmbeddedResourceContentBlock
	return jsonv2.Marshal(struct {
		Type string `json:"type"`
		base
	}{
		Type: b.Type(),
		base: base(b),
	})
}

// Binary VA: 0x10a08a6d0 | Size: 24 B
type ContentBlocks []ContentBlock

// Binary VA: 0x10025f100 | Size: 1248 B | Source: third_party/jetski/cli/acp/messages/content.go:L136
func (c *ContentBlocks) UnmarshalJSON(data []byte) error {
	var raw []jsontext.Value
	if err := jsonv2.Unmarshal(data, &raw); err != nil {
		return err
	}
	blocks := make([]ContentBlock, 0, len(raw))
	for _, item := range raw {
		var rt rawType
		if err := jsonv2.Unmarshal(item, &rt); err != nil {
			return err
		}
		switch rt.Type {
		case "text":
			var b TextContentBlock
			if err := jsonv2.Unmarshal(item, &b); err != nil {
				return err
			}
			blocks = append(blocks, b)
		case "resource_link":
			var b ResourceLinkContentBlock
			if err := jsonv2.Unmarshal(item, &b); err != nil {
				return err
			}
			blocks = append(blocks, b)
		case "image":
			var b ImageContentBlock
			if err := jsonv2.Unmarshal(item, &b); err != nil {
				return err
			}
			blocks = append(blocks, b)
		case "resource":
			var b EmbeddedResourceContentBlock
			if err := jsonv2.Unmarshal(item, &b); err != nil {
				return err
			}
			blocks = append(blocks, b)
		default:
			return fmt.Errorf("unknown content block type: %s", rt.Type) /* inferred */
		}
	}
	*c = blocks
	return nil
}

// Binary VA: 0x10025f5e0 | Size: 1760 B | Source: third_party/jetski/cli/acp/messages/content.go:L190
func (c ContentBlocks) String() string {
	var sb strings.Builder
	for _, block := range c {
		switch b := block.(type) {
		case TextContentBlock:
			sb.WriteString(b.Text)
		case EmbeddedResourceContentBlock:
			if b.Resource.Text != "" { /* inferred */
				sb.WriteString(b.Resource.InlineText())
			}
		case ResourceLinkContentBlock:
			sb.WriteString(b.URI) /* inferred */
		}
	}
	return sb.String()
}

// Binary VA: 0x10a161d60 | Size: 16 B
type ToolCallContent interface {
	Type() string
	isToolCallContent()
}

// Binary VA: 0x10a327c70 | Size: 24 B
type ToolCallLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

// Binary VA: 0x10a658628 | Size: 48 B
type DiffToolCallContent struct {
	Path    string `json:"path"`
	NewText string `json:"newText"`
	OldText string `json:"oldText,omitempty"`
}

func (DiffToolCallContent) isToolCallContent() {}

// Binary VA: 0x10025fcc0 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/content.go:L237
func (DiffToolCallContent) Type() string {
	return "diff"
}

// Binary VA: 0x10025fcd0 | Size: 208 B | Source: third_party/jetski/cli/acp/messages/content.go:L240
func (d DiffToolCallContent) MarshalJSON() ([]byte, error) {
	type base DiffToolCallContent
	return jsonv2.Marshal(struct {
		Type string `json:"type"`
		base
	}{
		Type: d.Type(),
		base: base(d),
	})
}

// Binary VA: 0x10a47e490 | Size: 16 B
type ContentToolCallContent struct {
	Content ContentBlock `json:"content,omitempty"`
}

func (ContentToolCallContent) isToolCallContent() {}

// Binary VA: 0x10025fda0 | Size: 16 B | Source: third_party/jetski/cli/acp/messages/content.go:L258
func (ContentToolCallContent) Type() string {
	return "content"
}

// Binary VA: 0x10025fdb0 | Size: 160 B | Source: third_party/jetski/cli/acp/messages/content.go:L261
func (c ContentToolCallContent) MarshalJSON() ([]byte, error) {
	type base ContentToolCallContent
	return jsonv2.Marshal(struct {
		Type string `json:"type"`
		base
	}{
		Type: c.Type(),
		base: base(c),
	})
}
