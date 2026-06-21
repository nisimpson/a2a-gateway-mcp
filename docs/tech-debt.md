# Tech Debt

Tracked items that should be revisited when upstream dependencies or project constraints change.

## MCP SDK `oneOf` Output Validation

- **Area:** `gateway/tools.go`, `gateway/output_schemas.go`
- **Priority:** Medium (fix when SDK is updated)
- **Origin:** structured-responses spec review

**Description:** The MCP SDK (v1.6.1) cannot validate `oneOf` in output schemas at runtime. As a result, `handleSendMessage` returns `any` instead of `*SendMessageResponse`. The schema validates correctly in standalone tests (proven in `internal/specification/validate_test.go`) but the SDK's internal `applySchema` fails when encountering `oneOf` branches.

**Resolution:** When the MCP Go SDK adds support for `oneOf` validation in output schemas, change `handleSendMessage` return type from `any` to `*SendMessageResponse`.
