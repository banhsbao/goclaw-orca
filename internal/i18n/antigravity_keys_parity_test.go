package i18n

import (
	"strings"
	"testing"
)

func TestI18nCatalogs_HasAntigravityKeys(t *testing.T) {
	keys := []string{MsgAgyNotConfigured, MsgAgyNoLoginSession, MsgAgyLoginFailed, MsgAgyInvalidCode}
	for _, locale := range []string{LocaleEN, LocaleVI, LocaleZH, LocaleKO, LocaleRU} {
		for _, key := range keys {
			msg := lookup(locale, key)
			if msg == key || strings.TrimSpace(msg) == "" {
				t.Errorf("locale=%s key=%s missing translation", locale, key)
			}
		}
	}
}
