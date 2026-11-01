package main

import (
	"reflect"
	"regexp"
	"slices"
	"testing"
)

// TestTextsComplete: every language in uiLangs has a full set of texts, and
// each string keeps the format verbs of the German master in the same order.
func TestTextsComplete(t *testing.T) {
	verbs := regexp.MustCompile(`%[-+ #0]*[0-9]*(\.[0-9]+)?[a-z%]`)
	de := reflect.ValueOf(textsDE)
	for _, lang := range uiLangs {
		tx, ok := textsByLang[lang]
		if !ok {
			t.Errorf("%s: not in textsByLang", lang)
			continue
		}
		if tx.lang != lang {
			t.Errorf("%s: lang field is %q", lang, tx.lang)
		}
		v := reflect.ValueOf(*tx)
		for i := 0; i < v.NumField(); i++ {
			name, f := v.Type().Field(i).Name, v.Field(i)
			switch f.Kind() {
			case reflect.String:
				if f.String() == "" {
					t.Errorf("%s.%s is empty", lang, name)
				}
				if want, got := verbs.FindAllString(de.Field(i).String(), -1), verbs.FindAllString(f.String(), -1); !slices.Equal(want, got) {
					t.Errorf("%s.%s format verbs %v, German has %v", lang, name, got, want)
				}
			case reflect.Func:
				if f.IsNil() {
					t.Errorf("%s.%s is nil", lang, name)
				}
			}
		}
	}
	if len(textsByLang) != len(uiLangs) {
		t.Errorf("textsByLang has %d languages, uiLangs %d", len(textsByLang), len(uiLangs))
	}
}
