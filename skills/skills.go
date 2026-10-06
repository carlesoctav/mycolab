// Package skills bundles the agent-facing SKILL.md files shipped with
// mycolab, so `mycolab skill` prints them anywhere without needing the
// repo checkout at runtime.
package skills

import _ "embed"

//go:embed mycolab/SKILL.md
var Mycolab string
