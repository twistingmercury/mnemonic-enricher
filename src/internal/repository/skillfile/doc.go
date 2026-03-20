// Package skillfile provides the PostgreSQL repository implementation for
// skill file persistence. Skill files store child files (scripts, references,
// assets) associated with skill definitions. Unlike agents and skills, skill
// files do not use the JSONB document model; they store file content directly
// in a TEXT column with a path column for identification within the skill
// directory.
//
// The repository implements CRUD operations, skill-scoped listing, and a
// manifest endpoint for the sync protocol.
//
// Documentation:
//   - Architecture: docs/architecture/04-data-architecture.md
//   - Design: docs/design/data-storage.md (Repository Interfaces > SkillFileRepository)
package skillfile
