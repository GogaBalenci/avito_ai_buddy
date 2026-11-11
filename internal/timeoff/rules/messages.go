package rules

import (
	"fmt"
	"time"
)

var weekdays = map[int]string{6: "суббота", 0: "воскресенье"}

func ddmm(s string) string {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return d.Format("02.01")
}

// Message is the user text of a violation (10 §7, A1–A7).
func Message(v Violation) string {
	str := func(k string) string { s, _ := v.Params[k].(string); return s }
	switch v.Code {
	case Required:
		return "Укажи дату"
	case InvalidDate:
		return "Не похоже на дату"
	case DateInPast:
		return "Дата уже прошла — выбери сегодня или позже"
	case TooFar:
		d, _ := time.Parse("2006-01-02", str("max_date"))
		return "Можно выбрать дату не позже " + d.Format("02.01.2006")
	case WeekendDate:
		wd, _ := v.Params["weekday"].(int)
		return fmt.Sprintf("%s — %s. Отгул начинается и заканчивается в рабочий день", ddmm(str("date")), weekdays[wd])
	case RangeReversed:
		return "«До» не может быть раньше «от»"
	case TooLong:
		n, _ := v.Params["working_days"].(int)
		return fmt.Sprintf("Выбрано %d рабочих дней, а в тестовом сервисе можно до 5. Для долгого отсутствия поговори с руководителем", n)
	case Overlap:
		return fmt.Sprintf("Пересекается с заявкой %s на %s–%s", str("request_id"), ddmm(str("date_from")), ddmm(str("date_to")))
	}
	return "Сервис отклонил даты: проверь их или спроси наставника"
}
