package users

// Validators for the language and CEFR codes the bot recognizes. They live in
// the users package because every other layer (articles, telegram/handlers)
// already depends on it, so this is the cheapest single source of truth.

// SupportedInterfaceLanguages is the canonical ordering of UI languages —
// used both by NormalizeLanguage on registration and to render the
// onboarding/settings interface-language pickers.
var SupportedInterfaceLanguages = []string{"ru", "en", "es"}

// SupportedLearningLanguages lists the languages a user can pick as a
// learning target. Order matches the keyboard shown in /settings.
var SupportedLearningLanguages = []string{"en", "es", "ru"}

// CEFRLevels is the canonical A1..C2 ordering, used both for membership
// checks and for shifting one level up/down.
var CEFRLevels = []string{"A1", "A2", "B1", "B2", "C1", "C2"}

// IsSupportedInterfaceLanguage reports whether code is one of the bot's UI
// languages.
func IsSupportedInterfaceLanguage(code string) bool {
	for _, l := range SupportedInterfaceLanguages {
		if l == code {
			return true
		}
	}
	return false
}

// IsSupportedLearningLanguage reports whether code is a language the user
// is allowed to study.
func IsSupportedLearningLanguage(code string) bool {
	for _, l := range SupportedLearningLanguages {
		if l == code {
			return true
		}
	}
	return false
}

// IsCEFR reports whether s is a known CEFR level (A1..C2, exact case).
func IsCEFR(s string) bool {
	for _, l := range CEFRLevels {
		if l == s {
			return true
		}
	}
	return false
}

// CEFRShift returns the level `delta` steps above (positive) or below
// (negative) `base`. ok is false when `base` is unknown or the shift falls
// off the A1..C2 range.
func CEFRShift(base string, delta int) (string, bool) {
	for i, l := range CEFRLevels {
		if l != base {
			continue
		}
		j := i + delta
		if j < 0 || j >= len(CEFRLevels) {
			return "", false
		}
		return CEFRLevels[j], true
	}
	return "", false
}
