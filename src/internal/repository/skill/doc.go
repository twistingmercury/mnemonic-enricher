// Package skill provides the PostgreSQL repository implementation for skill
// persistence. Skills are stored using the JSONB document model: only the
// fields required for database-level operations (name, crc64, timestamps) are
// top-level columns, with the full skill specification in a single JSONB
// definition column.
//
// The repository implements CRUD operations, pagination, existence checks,
// and a manifest endpoint for the sync protocol.
//
// Documentation:
//   - Architecture: docs/architecture/04-data-architecture.md
//   - Design: docs/design/data-storage.md (Repository Interfaces > SkillRepository)
package skill
