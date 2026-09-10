// Package skills carries the skill sources and the CLAUDE.md directive that
// `worklog install` deploys, compiled into the binary.
//
// They live here, one level under the module root, because go:embed cannot
// reach outside its module: the three workflow skills used to sit at the repo
// root, above worklog/, which put them permanently out of reach. Moving them
// down is what makes a clone-free install possible at all — the binary is now
// the delivery vehicle for its own skills, so a machine with neither Go nor a
// checkout still gets the full set.
//
// The move has a second effect worth knowing about. installer.RepoRev scopes
// its dirty check to the worklog/ subtree, so a skill edit at the repo root
// used to leave the rev clean: stale bytes would deploy while --check reported
// all clear. Now that skills are inside that subtree, editing one dirties the
// rev and stales the binary, which is correct — the skills ARE the binary now
// — but it does mean editing prose triggers a rebuild.
package skills

import "embed"

// FS holds the deploy sources, rooted at this directory.
//
// The directories are named one by one rather than globbed. The plain
// //go:embed form silently skips names beginning with "." or "_" and just as
// silently absorbs anything else that turns up in the tree, which is how a
// developer's stray directory ends up inside a release. Naming them means a
// new skill has to be added here on purpose, and TestEmbeddedSkillManifest
// pins the exact file list so neither accident can pass.
//
// CLAUDE.md is the directive body that `worklog install --with-claude-md`
// writes into the user's global config. The copy at the repo root is a symlink
// to this file: go:embed refuses to follow symlinks, so the real file has to
// be the one in here, and only that direction builds.
//
//go:embed CLAUDE.md dev-context contract fan-out worklog
var FS embed.FS
