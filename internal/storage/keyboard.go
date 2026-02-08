package storage

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"handler/internal/tg"
)

func (w *WatchItem) SeriesKeyboard() *tg.InlineKeyboardMarkup {
	return w.SeriesKeyboardWithVoice("")
}

func (w *WatchItem) SeriesKeyboardWithVoice(voice string) *tg.InlineKeyboardMarkup {
	if w == nil {
		return nil
	}
	voice = strings.TrimSpace(voice)
	voiceKey := url.QueryEscape(voice)
	rows := make([][]tg.InlineKeyboardButton, 0, len(w.Seasons)+1)
	for _, s := range w.Seasons {
		if voice != "" && !seasonHasVoice(&s, voice) {
			continue
		}
		cb := fmt.Sprintf("season:%d:%d", w.KPID, s.Number)
		if voiceKey != "" {
			cb = fmt.Sprintf("season:%d:%d:%s", w.KPID, s.Number, voiceKey)
		}
		rows = append(rows, []tg.InlineKeyboardButton{{Text: fmt.Sprintf("%d сезон", s.Number), CallbackData: cb}})
	}
	rows = append(rows, []tg.InlineKeyboardButton{
		{Text: "Назад", CallbackData: fmt.Sprintf("seriesvoices:%d", w.KPID)},
		{Text: "Закрыть", CallbackData: "close"},
	})
	kb := tg.NewInlineKeyboardMarkup(rows)
	return &kb
}

func (w *WatchItem) SeasonKeyboard(seasonNum int, page int, voice string) *tg.InlineKeyboardMarkup {
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

	voice = strings.TrimSpace(voice)
	voiceKey := url.QueryEscape(voice)

	if page < 1 {
		page = 1
	}
	const perPage = 24
	filtered := make([]Episode, 0, len(season.Episodes))
	for _, ep := range season.Episodes {
		if voice == "" {
			filtered = append(filtered, ep)
			continue
		}
		if episodeHasVoice(&ep, voice) {
			filtered = append(filtered, ep)
		}
	}
	total := len(filtered)
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
		ep := filtered[i]
		cbVoice := voiceKey
		if cbVoice == "" && len(ep.Variants) > 1 {
			cbVoice = "select"
		}
		row = append(row, tg.InlineKeyboardButton{
			Text:         fmt.Sprintf("%d серия", ep.Number),
			CallbackData: fmt.Sprintf("ep:%d:%d:%d:%s", w.KPID, seasonNum, ep.Number, cbVoice),
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
			if voiceKey != "" {
				nav = append(nav, tg.InlineKeyboardButton{Text: "<<<", CallbackData: fmt.Sprintf("seasonpage:%d:%d:%d:%s", w.KPID, seasonNum, page-1, voiceKey)})
			} else {
				nav = append(nav, tg.InlineKeyboardButton{Text: "<<<", CallbackData: fmt.Sprintf("seasonpage:%d:%d:%d", w.KPID, seasonNum, page-1)})
			}
		}
		if page < totalPages {
			if voiceKey != "" {
				nav = append(nav, tg.InlineKeyboardButton{Text: ">>>", CallbackData: fmt.Sprintf("seasonpage:%d:%d:%d:%s", w.KPID, seasonNum, page+1, voiceKey)})
			} else {
				nav = append(nav, tg.InlineKeyboardButton{Text: ">>>", CallbackData: fmt.Sprintf("seasonpage:%d:%d:%d", w.KPID, seasonNum, page+1)})
			}
		}
		if len(nav) > 0 {
			rows = append(rows, nav)
		}
	}

	if voiceKey != "" {
		rows = append(rows, []tg.InlineKeyboardButton{
			{Text: "Назад", CallbackData: fmt.Sprintf("seriesvoice:%d:%s", w.KPID, voiceKey)},
			{Text: "Закрыть", CallbackData: "close"},
		})
	} else {
		rows = append(rows, []tg.InlineKeyboardButton{
			{Text: "Назад", CallbackData: fmt.Sprintf("watch:%d", w.KPID)},
			{Text: "Закрыть", CallbackData: "close"},
		})
	}
	kb := tg.NewInlineKeyboardMarkup(rows)
	return &kb
}

func seasonHasVoice(season *Season, voice string) bool {
	if season == nil {
		return false
	}
	for i := range season.Episodes {
		if episodeHasVoice(&season.Episodes[i], voice) {
			return true
		}
	}
	return false
}

func episodeHasVoice(ep *Episode, voice string) bool {
	if ep == nil {
		return false
	}
	voice = strings.TrimSpace(voice)
	if voice == "" {
		return false
	}
	if len(ep.Variants) > 0 {
		for _, v := range ep.Variants {
			if strings.EqualFold(strings.TrimSpace(v.Voice), voice) {
				return true
			}
		}
		return false
	}
	return strings.EqualFold(strings.TrimSpace(ep.Voice), voice)
}

func uniqueSeasonVoices(season *Season) []string {
	if season == nil {
		return nil
	}
	seen := map[string]struct{}{}
	voices := []string{}
	for _, ep := range season.Episodes {
		if len(ep.Variants) > 0 {
			for _, v := range ep.Variants {
				name := strings.TrimSpace(v.Voice)
				if name == "" {
					continue
				}
				key := strings.ToLower(name)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				voices = append(voices, name)
			}
		} else {
			v := strings.TrimSpace(ep.Voice)
			if v == "" {
				continue
			}
			key := strings.ToLower(v)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			voices = append(voices, v)
		}
	}
	sort.Strings(voices)
	return voices
}
