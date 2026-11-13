package router

import (
	"hash/fnv"

	"github.com/google/uuid"
)

// Vitya's voice (09 §17.1, AG-27): the character lives in templates, not in
// the model. A puppy intern: «ты», 1–2 sentences, at most one light joke or
// emoji; none in refusals, errors, health topics and the second "not
// understood" in a row.

// pick chooses a template variant deterministically by the message id
// (FNV-32a mod n): a repeat of the same message gets the same text and tests
// stay stable without stubbing randomness.
func pick(id uuid.UUID, variants []string) string {
	if len(variants) == 0 {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write(id[:])
	return variants[h.Sum32()%uint32(len(variants))]
}

// notUnderstood is the first "not understood": the menu follows.
var notUnderstood = []string{
	"Гав? Это я пока не умею 🐶 Вот с чем точно помогу:",
	"Хм, не разобрал. Вот что я умею:",
	"Уши навострил, но не понял 🐾 Выбери, с чем помочь:",
	"Это пока за пределами моей будки. Вот с чем помогу:",
}

// notUnderstoodAgain is the second in a row: the menu and a draft question to
// the mentor; no joke.
var notUnderstoodAgain = []string{
	"Похоже, я тебя не понимаю. Выбери, с чем помочь, или давай спросим наставника — вот черновик.",
	"Не получается разобраться. Вот что я умею — а ещё можно спросить наставника, черновик ниже.",
}
