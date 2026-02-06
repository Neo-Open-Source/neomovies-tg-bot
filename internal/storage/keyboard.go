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

func (w *WatchItem) SeasonKeyboard(seasonNum int, page int) *tg.InlineKeyboardMarkup {
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

	if page < 1 {
		page = 1
	}
	const perPage = 24
	total := len(season.Episodes)
	totalPages := (total + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * perPage
	end := start + perPage
	if end > total {
		end = total
	}

	rows := make([][]tg.InlineKeyboardButton, 0, perPage/3+4)
	// Header row (tap to go back to season list)
	rows = append(rows, []tg.InlineKeyboardButton{{Text: fmt.Sprintf("%d сезон", seasonNum), CallbackData: fmt.Sprintf("watch:%d", w.KPID)}})

	row := []tg.InlineKeyboardButton{}
	for i := start; i < end; i++ {
		ep := season.Episodes[i]
		row = append(row, tg.InlineKeyboardButton{
			Text:         fmt.Sprintf("%d серия", ep.Number),
			CallbackData: fmt.Sprintf("ep:%d:%d:%d", w.KPID, seasonNum, ep.Number),
		})
		if len(row) == 3 {
			rows = append(rows, row)
			row = []tg.InlineKeyboardButton{}
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}

	if totalPages > 1 {
		nav := []tg.InlineKeyboardButton{}
		if page > 1 {
			nav = append(nav, tg.InlineKeyboardButton{Text: "<<<", CallbackData: fmt.Sprintf("seasonpage:%d:%d:%d", w.KPID, seasonNum, page-1)})
		}
		if page < totalPages {
			nav = append(nav, tg.InlineKeyboardButton{Text: ">>>", CallbackData: fmt.Sprintf("seasonpage:%d:%d:%d", w.KPID, seasonNum, page+1)})
		}
		if len(nav) > 0 {
			rows = append(rows, nav)
		}
	}

	rows = append(rows, []tg.InlineKeyboardButton{{Text: "Закрыть", CallbackData: "close"}})
	kb := tg.NewInlineKeyboardMarkup(rows)
	return &kb
}
