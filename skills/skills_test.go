package skills

import (
	"strings"
	"testing"
)

func TestMycolabSkillEmbedded(t *testing.T) {
	for _, want := range []string{
		"name: mycolab",
		"## Flow A",
		"## Flow B",
		"mycolab new -s",
		"mycolab run -s",
		"mycolab skill",
	} {
		if !strings.Contains(Mycolab, want) {
			t.Errorf("embedded skill missing %q", want)
		}
	}
}
