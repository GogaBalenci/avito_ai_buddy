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

// smallTalkReplies are chit-chat templates by kind (09 §17.3, AG-29); the
// second sentence (back to work) is added by smallTalk.
var smallTalkReplies = map[string][]string{
	"greeting":    {"Привет! 🐶 Я Витя, твой бадди на стажировке.", "Гав, привет! Рад тебя видеть.", "Привет-привет! Хвост уже виляет."},
	"how_are_you": {"Отлично, хвост виляет! 🐶", "Бодро: с утра обнюхал весь чек-лист.", "Хорошо! Стажируюсь вместе с тобой."},
	"who_are_you": {
		"Я Витя — Авиитик, щенок-стажёр и твой бадди. Да, я бот, но очень старательный 🐶",
		"Я бот-помощник Витя: подсказываю шаги адаптации, отвечаю по тестовой базе знаний и оформляю заявки.",
		"Витя, бадди-бот. Авиитик — от «ИИ», а Витя — для краткости.",
	},
	"thanks":     {"Всегда пожалуйста! 🐾", "Рад помочь — это моя любимая команда.", "Гав! То есть пожалуйста."},
	"bye":        {"Пока! Если что — я рядом 🐶", "До встречи! Посторожу твой чек-лист.", "Удачи! Возвращайся с вопросами."},
	"music":      {"Больше всего люблю шуршание пакета с кормом 🎵", "Писк игрушки-косточки и звук одобренной заявки.", "Гитарное соло виляющего хвоста — мой жанр."},
	"likes":      {"Люблю, когда шаги становятся «Готово» 🐶", "Обожаю закрывать чек-листы и грызть сложные вопросы.", "Прогулки, косточки и понятные инструкции."},
	"compliment": {"Спасибо, у меня аж уши встали 🐶", "Приятно! Передам хвосту — пусть виляет.", "Ты тоже молодец!"},
	"joke": {
		"Мой любимый язык — Go: на нём «апорт» пишется go fetch 🐶",
		"Как щенок делает ревью? Сначала обнюхивает diff, потом виляет approve.",
		"Лучший коммит в моей истории — «fix: поймал свой хвост».",
	},
}
