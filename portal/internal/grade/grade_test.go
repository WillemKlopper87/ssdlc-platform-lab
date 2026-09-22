package grade

import "testing"

func TestScoreAndLetter(t *testing.T) {
	for _, c := range []struct {
		name   string
		counts Counts
		score  int
		letter string
	}{
		{"clean", Counts{}, 100, "A"},
		{"one low", Counts{Low: 1}, 98, "A"},
		{"boundary A", Counts{Medium: 1, Low: 2}, 90, "A"},
		{"one high two medium", Counts{High: 1, Medium: 2}, 70, "C"},
		{"one high", Counts{High: 1}, 82, "B"},
		{"boundary B", Counts{High: 1, Medium: 1, Low: 0}, 76, "B"},
		{"one critical", Counts{Critical: 1}, 65, "C"},
		{"critical and high", Counts{Critical: 1, High: 1}, 47, "D"},
		{"boundary D", Counts{Critical: 1, High: 1, Low: 3}, 41, "D"},
		{"two critical", Counts{Critical: 2}, 30, "F"},
		{"negative stays F", Counts{Critical: 5}, -75, "F"},
	} {
		s := Score(c.counts)
		if s != c.score {
			t.Errorf("%s: Score = %d, want %d", c.name, s, c.score)
		}
		if l := Letter(s); l != c.letter {
			t.Errorf("%s: Letter(%d) = %s, want %s", c.name, s, l, c.letter)
		}
	}
}

func TestLetterBoundaries(t *testing.T) {
	for score, want := range map[int]string{90: "A", 89: "B", 75: "B", 74: "C", 60: "C", 59: "D", 40: "D", 39: "F"} {
		if got := Letter(score); got != want {
			t.Errorf("Letter(%d) = %s, want %s", score, got, want)
		}
	}
}

func TestSLADays(t *testing.T) {
	for sev, want := range map[string]int{"critical": 2, "HIGH": 7, "Medium": 30, "low": 90, "info": 0, "": 0} {
		if got := SLADays(sev); got != want {
			t.Errorf("SLADays(%q) = %d, want %d", sev, got, want)
		}
	}
}
