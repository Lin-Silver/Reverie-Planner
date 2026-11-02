# AI provider settings

Settings → AI providers can add, edit or delete OpenAI-compatible endpoints.
Local models can use a loopback HTTP address; remote services require HTTPS.
A model, label and endpoint are required. AI is optional for all core workflows.

API keys are never returned in the settings response. An empty key field retains
the current saved key; the separate checkbox explicitly removes it. Keys use the
existing platform protection and local configuration file, whose portability
limitations are described in PORTABLE.md. Deleting a provider deletes its
credential from the current provider configuration. Testing is a deliberate
button action on saved settings and sends a short model request; remote usage can
consume quota. Automated tests use synthetic data and mocked connections.

The settings form reads GET `/api/provider-management`, then submits a PUT with
the complete edited list and the returned revision. App-managed writes serialize
with legacy provider writes; an outdated revision receives 409 and keeps the
unsaved draft until the user explicitly reloads. The revision derives from the
stored configuration bytes, not a secret returned to the frontend. Writes use the
existing protected temporary file and atomic rename. This coordination covers
writes through this application process; editing the file with an external tool
at the same time is outside that guarantee. A 500 after a successful write but
failed readback asks for a reload, rather than claiming a rollback.

Unchanged legacy entries are retained even if their older numeric defaults no
longer meet the validation for newly edited entries.

The connection test looks up the saved provider by ID; a missing ID returns 404
instead of testing another provider. The new management screen currently supports
OpenAI-compatible providers only. Non-AI offline functions remain independent.
