// Binary VA: 0x102f8a240..0x102f8bc20 | Source: third_party/jetski/cli/acp/server/promptcontent.go

package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"google3/base/go/log"
	"google3/third_party/jetski/cli/acp/messages/messages"
	"google3/third_party/jetski/cli/slashcmd/slashcmd"
	"google3/third_party/jetski/codeium_common_pb/codeium_common_go_proto"
	"google3/third_party/jetski/language_server_pb/language_server_go_proto"
)

// Binary VA: 0x102f8a240 | Size: 3648 B | Source: third_party/jetski/cli/acp/server/promptcontent.go:L28
func promptToUserMessage(ctx context.Context, blocks []messages.ContentBlock, slashCmds func() []*language_server_go_proto.SlashCommandDefinition) ([]*codeium_common_go_proto.TextOrScopeItem, []*codeium_common_go_proto.Media, error) {
	var items []*codeium_common_go_proto.TextOrScopeItem
	var media []*codeium_common_go_proto.Media

	for i, block /* inferred */ := range blocks {
		switch b /* inferred */ := block.(type) {
		case messages.TextContentBlock:
			if b.Text == "" {
				continue
			}
			if len(items) == 0 && len(media) == 0 && slashCmds != nil && strings.Contains(b.Text, "/") {
				items = append(items, expandSlashCommands(b.Text, slashCmds())...)
				continue
			}
			items = append(items, &codeium_common_go_proto.TextOrScopeItem{
				Chunk: &codeium_common_go_proto.TextOrScopeItem_Text{Text: b.Text},
			})

		case messages.ResourceLinkContentBlock:
			if b.URI != "" {
				items = append(items, fileItem(b.URI))
			}

		case messages.ImageContentBlock:
			m, err := inlineMedia(b.MimeType, b.Data, b.URI)
			if err != nil {
				return nil, nil, fmt.Errorf("prompt[%d]: image: %w", i, err)
			}
			media = append(media, m)

		case messages.EmbeddedResourceContentBlock:
			res /* inferred */ := b.Resource
			if res.Blob != "" {
				if !isImageMimeType(res.MimeType) {
					log.WarningContextf(ctx, "Skipping embedded resource %s: unsupported blob type %q", res.URI, res.MimeType)
					continue
				}
				m, err := inlineMedia(res.MimeType, res.Blob, res.URI)
				if err != nil {
					return nil, nil, fmt.Errorf("prompt[%d]: resource %s: %w", i, res.URI, err)
				}
				media = append(media, m)
			} else if res.Text == "" && isWholeFileURI(res.URI) {
				items = append(items, fileItem(res.URI))
			} else if text /* inferred */ := res.InlineText(); text != "" {
				items = append(items, &codeium_common_go_proto.TextOrScopeItem{
					Chunk: &codeium_common_go_proto.TextOrScopeItem_Text{Text: text},
				})
			} else {
				log.WarningContextf(ctx, "Skipping embedded resource %s: no text or blob", res.URI)
			}
		}
	}

	return items, media, nil
}

// Binary VA: 0x102f8b080 | Size: 112 B | Source: third_party/jetski/cli/acp/server/promptcontent.go:L83
func expandSlashCommands(text string, defs []*language_server_go_proto.SlashCommandDefinition) []*codeium_common_go_proto.TextOrScopeItem {
	resolvers, _ /* inferred */ := slashcmd.AdvertisedResolvers(defs)
	return slashcmd.Expand(text, resolvers)
}

// Binary VA: 0x102f8b0f0 | Size: 976 B | Source: third_party/jetski/cli/acp/server/promptcontent.go:L93
func planPrompt(ctx context.Context, blocks []messages.ContentBlock, slashCmds func() []*language_server_go_proto.SlashCommandDefinition) []messages.ContentBlock {
	_, resolver /* inferred */ := slashcmd.AdvertisedResolvers(slashCmds())
	if resolver("plan") == nil {
		log.WarningContextf(ctx, "Plan mode has no effect: the session does not offer /%s", "plan")
		return blocks
	}
	for len(blocks) > 0 && blocks[0] == (messages.TextContentBlock{}) {
		blocks = blocks[1:]
	}
	if len(blocks) == 0 {
		return blocks
	}
	if first, ok := blocks[0].(messages.TextContentBlock); ok {
		text /* inferred */ := first.Text
		if _, hasSlash /* inferred */ := slashcmd.LeadingName(text); !hasSlash {
			text = "/plan " + text
		}
		return append([]messages.ContentBlock{messages.TextContentBlock{Text: text}}, blocks[1:]...)
	}
	return append([]messages.ContentBlock{messages.TextContentBlock{Text: "/plan"}}, blocks...)
}

// Binary VA: 0x102f8b4c0 | Size: 512 B | Source: third_party/jetski/cli/acp/server/promptcontent.go:L118
func fileItem(uri string) *codeium_common_go_proto.TextOrScopeItem {
	return &codeium_common_go_proto.TextOrScopeItem{
		Chunk: &codeium_common_go_proto.TextOrScopeItem_Item{
			Item: codeium_common_go_proto.ContextScopeItem_builder{
				File: &codeium_common_go_proto.PathScopeItem{
					AbsoluteUri: uri,
				},
			}.Build(),
		},
	}
}

// Binary VA: 0x102f8b6c0 | Size: 128 B | Source: third_party/jetski/cli/acp/server/promptcontent.go:L126
func isImageMimeType(mimeType string) bool {
	return strings.HasPrefix(normalizeMimeType(mimeType), "image/")
}

// Binary VA: 0x102f8b740 | Size: 80 B | Source: third_party/jetski/cli/acp/server/promptcontent.go:L131
func normalizeMimeType(mimeType string) string {
	return strings.ToLower(strings.TrimSpace(mimeType))
}

// Binary VA: 0x102f8b790 | Size: 624 B | Source: third_party/jetski/cli/acp/server/promptcontent.go:L138
func inlineMedia(mimeType, data, uri string) (*codeium_common_go_proto.Media, error) {
	if data == "" {
		return nil, errors.New("data is required")
	}
	if !isImageMimeType(mimeType) {
		return nil, fmt.Errorf("unsupported mimeType %q", mimeType)
	}
	decoded, err := decodeBase64(data)
	if err != nil {
		return nil, fmt.Errorf("data is not valid base64: %w", err)
	}
	return codeium_common_go_proto.Media_builder{
		MimeType:   normalizeMimeType(mimeType),
		Uri:        uri,
		InlineData: decoded,
	}.Build(), nil
}

// Binary VA: 0x102f8ba00 | Size: 384 B | Source: third_party/jetski/cli/acp/server/promptcontent.go:L158
func decodeBase64(data string) ([]byte, error) {
	cleaned := strings.NewReplacer("\r", "", "\n", "").Replace(data)
	b, err := base64.StdEncoding.DecodeString(cleaned)
	if err == nil {
		return b, nil
	}
	if raw, rawErr := base64.RawStdEncoding.DecodeString(cleaned); rawErr == nil {
		return raw, nil
	}
	return nil, err
}

// Binary VA: 0x102f8bb80 | Size: 160 B | Source: third_party/jetski/cli/acp/server/promptcontent.go:L172
func isWholeFileURI(rawURI string) bool {
	u, err := url.Parse(rawURI)
	if err != nil {
		return false
	}
	return u.Scheme == "file" && u.Path != "" && u.Path != "/" && u.Fragment == "" && u.RawQuery == ""
}
