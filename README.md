# Kino Core

Kino Core is the provider layer for Kino Android. It is a Go library distributed as an Android AAR. Android owns account storage, chat history, settings, model selection, and UI state. The core receives those values per call, performs provider protocol work, and returns structured results and stream events.

## Stateless boundary

The core does not save credentials, conversations, settings, or provider sessions. For each request Android passes:

- Provider credentials.
- A stable, random device ID for DeepSeek settings.
- The selected model type and thinking/search options.
- The DeepSeek chat-session ID and parent message ID for follow-up turns.

DeepSeek’s website retains its own conversation state. Android stores the session ID and latest response message ID alongside the local transcript, then supplies them on the next request. The core keeps only a reusable HTTP transport for connection pooling; proof-of-work challenge values are fetched and solved per request.

Credentials are sensitive browser-session values. Android should keep them in Keystore-backed encrypted storage and pass them to the core only when needed. They are never logged or persisted by this library.

## DeepSeek features

- Versioned model and feature settings, including model availability, thinking/search support, file support, and configured context/file limits.
- Session creation and streamed completion, with typed events for text, thinking, search fragments/results, status, message IDs, and context token usage.
- Remote history and session-list pagination, rename, pin, delete, and feedback operations.
- File upload and file-status retrieval; the upload uses a streaming multipart body.
- Stop generation, cancellation, HTTP/2, bounded parallel proof-of-work, and large SSE event support.

`accumulated_token_usage` is the provider’s conversation-branch context counter. It is not an input/output billing breakdown. File parsing can also return a per-file `token_usage`. The core preserves these values and the current context limits for Android to display.

The website login credential is not an official DeepSeek API key. This adapter speaks the website API and its browser-session proof-of-work protocol. The protocol can change independently of the official API.

## Modules

- `core`: provider-neutral contracts and shared mobile-safe models.
- `core/pow`: DeepSeekHashV1 challenge hashing and a cancellable solver.
- `core/sse`: bounded-memory Server-Sent Events parser.
- DeepSeek adapter in `core`: account-specific settings, chat, history, session, and file operations.
- `mobile`: narrow JSON and callback API exported to Kotlin with `gomobile bind`.

New providers implement `ProviderAdapter` and register under a stable provider name. They return the same model/capability types and event stream, so Android does not need provider-specific persistence or screen code.

## Build and test

```sh
go test ./...
go test -race ./...
go vet ./...
```

Live integration testing is opt-in and reads an existing Kino Server account store without printing credential values:

```sh
KINO_CORE_CREDENTIALS_FILE=/path/to/accounts.json go test ./core -run '^TestLiveDeepSeek$' -count=1 -v
```

The live test loads capabilities, sends a completion, fetches history and context usage, continues with thinking enabled, and sends a search-enabled follow-up. It creates remote test conversations. It is skipped by default and is not run in CI.

CI builds the Android AAR for ARMv7, ARM64, x86, and x86_64. Versioned releases attach the AAR and its SHA-256 checksum; the Kino Android build downloads a pinned release rather than tracking generated binaries in the app repository.
