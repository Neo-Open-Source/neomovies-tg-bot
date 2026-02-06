package storage

import (
	"fmt"

	"handler/internal/tg"
)

func (w *WatchItem) SeriesKeyboard() *tg.InlineKeyboardMarkup {
	if w == nil {
		return nil
	}
	rows := make([][]tg.InlineKeyboardButton, 0, len(w.Seasons)+1)
	for _, s := range w.Seasons {
		rows = append(rows, []tg.InlineKeyboardButton{{Text: fmt.Sprintf("%d сезон", s.Number), CallbackData: fmt.Sprintf("season:%d:%d", w.KPID, s.Number)}})
	}
	rows = append(rows, []tg.InlineKeyboardButton{{Text: "Закрыть", CallbackData: "close"}})
	kb := tg.NewInlineKeyboardMarkup(rows)
	return &kb
}

func (w *WatchItem) SeasonKeyboard(seasonNum int) *tg.InlineKeyboardMarkup {
	if w == nil {
		return nil
	}
	var season *Season
	for i := range w.Seasons {
		if w.Seasons[i].Number == seasonNum {
			season = &w.Seasons[i]
			break
		}
	}
	if season == nil {
		return w.SeriesKeyboard()
	}

	rows := make([][]tg.InlineKeyboardButton, 0, len(season.Episodes)+2)
	for _, ep := range season.Episodes {
		rows = append(rows, []tg.InlineKeyboardButton{{Text: fmt.Sprintf("%d серия", ep.Number), CallbackData: fmt.Sprintf("ep:%d:%d:%d", w.KPID, seasonNum, ep.Number)}})
	}
	rows = append(rows, []tg.InlineKeyboardButton{{Text: "Назад", CallbackData: fmt.Sprintf("watch:%d", w.KPID)}})
	rows = append(rows, []tg.InlineKeyboardButton{{Text: "Закрыть", CallbackData: "close"}})
	kb := tg.NewInlineKeyboardMarkup(rows)
	return &kb
}
