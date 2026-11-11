package pii

import "testing"

func TestMask(t *testing.T) {
	cases := []struct {
		in, want string
		masked   bool
	}{
		{"мой номер карты 4276 1234 5678 9012", "мой номер карты [скрыто]", true}, // T-24
		{"карта 4276123456789012", "карта [скрыто]", true},
		{"звони +7 (912) 345-67-89", "звони [скрыто]", true},
		{"телефон 89123456789", "телефон [скрыто]", true},
		{"почта ivan.petrov@example.com", "почта [скрыто]", true},
		{"паспорт 45 06 123456", "паспорт [скрыто]", true},
		{"снилс 123-456-789 01", "снилс [скрыто]", true},
		// left intact
		{"что значит шаг 1.13", "что значит шаг 1.13", false},
		{"что с заявкой ABS-000123?", "что с заявкой ABS-000123?", false},
		{"заявка ACC-000101", "заявка ACC-000101", false},
		{"отгул 13.11–14.11", "отгул 13.11–14.11", false},
		{"с 2026-11-13 по 2026-11-14", "с 2026-11-13 по 2026-11-14", false},
		{"шаг be-1.12 и fe-w1-40", "шаг be-1.12 и fe-w1-40", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, masked := Mask(c.in)
		if got != c.want || masked != c.masked {
			t.Errorf("Mask(%q) = %q, %v; want %q, %v", c.in, got, masked, c.want, c.masked)
		}
	}
}
