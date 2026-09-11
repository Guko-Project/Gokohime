# Omoi media bridge

Deploy an Omoi version with the authenticated `/api/files` implementation before rebuilding/replacing the bot. Existing Omoi address, agent and API-key configuration is reused. No new vision provider or QQ account is needed.

Group mentions, private messages and the group buffer retain image/video references. Image-only messages are valid. Direct/replied attachments are required; bounded historical attachments are labelled, and missing historical media is explicitly reported to the model rather than blocking unrelated new text. A request accepts up to four attachments, including at most one video. Normal group messages still obey the existing probability/trigger configuration.

The bridge downloads only public QQ/CDN HTTP(S) URLs, validates redirects, pins public resolved IPs and blocks local/private/link-local endpoints. Base64 OneBot images are accepted within the same size limit. Arbitrary filesystem paths are never read. Missing OneBot URLs can be resolved through the local adapter. If QQ cannot provide a downloadable URL, the error is surfaced instead of silently discarding the attachment.

Downloads stream to temporary files under 20 MiB for images / 50 MiB for videos; Omoi performs image compression and video extraction. Temporary files are removed on success and error. Successfully uploaded references are reused for retries and buffered messages. Each chat has a 180-second deadline; incomplete SSE streams are treated as failures.

Each request carries a trace ID through upload and chat. QQ receives a short failure explanation plus that ID; server logs contain the detailed media stage and error. Download logs contain the domain and byte count, not the signed URL. Reply splitting and transport-format instructions already present in this plugin are preserved.

```sh
go test -race ./plugin/omoi
go build ./cmd/bot
```

Final QQ acceptance after deployment: private image-only; group @ + image; text with multiple images; reply to an image/video; image in recent group history; video overview; a follow-up requesting a readable frame; failed/oversized download; session recreation without losing attachments; text commands and split replies unchanged.
