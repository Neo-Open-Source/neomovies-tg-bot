package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"handler/internal/neomovies"
	"handler/internal/storage"
	"handler/internal/tg"
)

type update struct {
	UpdateID      int             `json:"update_id"`
	InlineQuery   *inlineQuery    `json:"inline_query"`
	ChosenInline  *chosenInline   `json:"chosen_inline_result"`
	CallbackQuery *callbackQuery  `json:"callback_query"`
	Message       *message        `json:"message"`
	MyChatMember  json.RawMessage `json:"my_chat_member"`
}

type user struct {
	ID int64 `json:"id"`
}

type inlineQuery struct {
	ID    string `json:"id"`
	From  user   `json:"from"`
	Query string `json:"query"`
}

type chosenInline struct {
	ResultID        string `json:"result_id"`
	From            user   `json:"from"`
	Query           string `json:"query"`
	InlineMessageID string `json:"inline_message_id"`
}

type callbackQuery struct {
	ID              string   `json:"id"`
	From            user     `json:"from"`
	Data            string   `json:"data"`
	Message         *message `json:"message"`
	InlineMessageID string   `json:"inline_message_id"`
}

type chat struct {
	ID int64 `json:"id"`
}

type message struct {
	MessageID            int      `json:"message_id"`
	Chat                 chat     `json:"chat"`
	Text                 string   `json:"text"`
	From                 *user    `json:"from"`
	ReplyToMessage       *message `json:"reply_to_message"`
	ForwardFromChat      *chat    `json:"forward_from_chat"`
	ForwardFromMessageID int      `json:"forward_from_message_id"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/library" || r.URL.Path == "/api/library/item" {
		libraryHandler(w, r)
		return
	}
	if r.URL.Path == "/api/player" {
		playerHandler(w, r)
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var upd update
	if err := json.Unmarshal(body, &upd); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("BOT_TOKEN is required"))
		return
	}

	apiBase := strings.TrimRight(os.Getenv("API_BASE"), "/")
	if apiBase == "" {
		apiBase = "https://api.neomovies.ru"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()

	bot := tg.NewClient(token)
	movies := neomovies.NewClient(apiBase)
	db, _ := storage.NewMongo(ctx, os.Getenv("MONGODB_URI"))

	switch {
	case upd.InlineQuery != nil:
		handleInlineQuery(ctx, w, bot, movies, db, upd.InlineQuery)
		return
	case upd.ChosenInline != nil:
		handleChosenInline(ctx, w, bot, movies, db, upd.ChosenInline)
		return
	case upd.CallbackQuery != nil:
		handleCallback(ctx, w, bot, movies, db, upd.CallbackQuery)
		return
	case upd.Message != nil:
		handleMessage(ctx, w, bot, movies, db, upd.Message)
		return
	default:
		w.WriteHeader(http.StatusOK)
		return
	}
}

type libraryItem struct {
	KPID          int      `json:"kp_id"`
	Type          string   `json:"type"`
	Title         string   `json:"title"`
	PosterURL     string   `json:"poster_url"`
	Rating        float64  `json:"rating"`
	Overview      string   `json:"overview"`
	Genres        []string `json:"genres,omitempty"`
	SeasonsCount  int      `json:"seasons_count,omitempty"`
	EpisodesCount int      `json:"episodes_count,omitempty"`
	Voices        []string `json:"voices,omitempty"`
}

func libraryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	apiBase := strings.TrimRight(os.Getenv("API_BASE"), "/")
	if apiBase == "" {
		apiBase = "https://api.neomovies.ru"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()

	movies := neomovies.NewClient(apiBase)
	db, _ := storage.NewMongo(ctx, os.Getenv("MONGODB_URI"))
	if db == nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if r.URL.Path == "/api/library/item" {
		kpID, _ := strconv.Atoi(r.URL.Query().Get("kp_id"))
		if kpID <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		out, err := buildLibraryItem(ctx, movies, item)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		writeJSON(w, out)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := db.ListRecent(ctx, limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	out := make([]libraryItem, 0, len(items))
	for i := range items {
		li, err := buildLibraryItem(ctx, movies, &items[i])
		if err != nil {
			continue
		}
		out = append(out, li)
	}
	writeJSON(w, out)
}

func buildLibraryItem(ctx context.Context, movies *neomovies.Client, wItem *storage.WatchItem) (libraryItem, error) {
	info, err := movies.GetMovieByKPID(ctx, wItem.KPID)
	if err != nil {
		return libraryItem{}, err
	}
	if info == nil {
		return libraryItem{}, fmt.Errorf("not found")
	}

	// Fallback to search by kp_id if API returned empty fields
	if info.Title == "" && info.NameRu == "" && info.Name == "" && info.NameOriginal == "" ||
		(info.Overview == "" && info.Description == "" && info.ShortDescription == "") {
		if sr, _ := movies.SearchMovies(ctx, strconv.Itoa(wItem.KPID), 1); sr != nil {
			for _, m := range sr.Results {
				matchID := m.ExternalIDs.KP
				if matchID == 0 {
					matchID = m.KinopoiskID
				}
				if matchID != wItem.KPID {
					continue
				}
				if info.Title == "" && info.NameRu == "" && info.Name == "" && info.NameOriginal == "" {
					info.Title = m.Title
					info.NameRu = m.NameRu
					info.Name = m.Name
					info.NameOriginal = m.NameOriginal
				}
				if info.Overview == "" && info.Description == "" && info.ShortDescription == "" {
					info.Overview = m.Overview
					info.Description = m.Description
					info.ShortDescription = m.ShortDescription
				}
				if len(info.Genres) == 0 && len(m.Genres) > 0 {
					info.Genres = m.Genres
				}
				if info.Rating == 0 && m.Rating > 0 {
					info.Rating = m.Rating
				}
				if info.RatingKinopoisk == 0 && m.RatingKinopoisk > 0 {
					info.RatingKinopoisk = m.RatingKinopoisk
				}
				if info.VoteAverage == 0 && m.VoteAverage > 0 {
					info.VoteAverage = m.VoteAverage
				}
				if info.Year == "" && m.Year != "" {
					info.Year = m.Year
				}
				if info.ReleaseDate == "" && m.ReleaseDate != "" {
					info.ReleaseDate = m.ReleaseDate
				}
				if info.ExternalIDs.KP == 0 && m.ExternalIDs.KP != 0 {
					info.ExternalIDs.KP = m.ExternalIDs.KP
				}
				if info.ExternalIDs.IMDB == "" && m.ExternalIDs.IMDB != "" {
					info.ExternalIDs.IMDB = m.ExternalIDs.IMDB
				}
				if info.PosterPath == "" && m.PosterPath != "" {
					info.PosterPath = m.PosterPath
				}
				if info.PosterURLPreview == "" && m.PosterURLPreview != "" {
					info.PosterURLPreview = m.PosterURLPreview
				}
				if info.PosterURL == "" && m.PosterURL != "" {
					info.PosterURL = m.PosterURL
				}
				break
			}
		}
	}

	title := strings.TrimSpace(firstNonEmpty(info.Title, info.NameRu, info.Name, info.NameOriginal, wItem.Title))
	if title == "" {
		title = fmt.Sprintf("kp_%d", wItem.KPID)
	}
	poster := firstNonEmpty(info.PosterPath, info.PosterURLPreview, info.PosterURL)
	posterURL := movies.ImageURL(poster, "kp", wItem.KPID)
	if posterURL == "" {
		apiBase := strings.TrimRight(os.Getenv("API_BASE"), "/")
		if apiBase == "" {
			apiBase = "https://api.neomovies.ru"
		}
		posterURL = fmt.Sprintf("%s/api/v1/images/kp/%d", apiBase, wItem.KPID)
	}
	rating := info.Rating
	if rating == 0 {
		rating = info.RatingKinopoisk
	}
	if rating == 0 {
		rating = info.VoteAverage
	}
	overview := strings.TrimSpace(firstNonEmpty(info.Overview, info.Description, info.ShortDescription))

	genres := make([]string, 0, len(info.Genres))
	for _, g := range info.Genres {
		name := strings.TrimSpace(g.Name)
		if name != "" {
			genres = append(genres, name)
		}
	}

	seasonsCount := 0
	episodesCount := 0
	if wItem.Type == "series" {
		seasonsCount = len(wItem.Seasons)
		for _, s := range wItem.Seasons {
			episodesCount += len(s.Episodes)
		}
	}

	voices := []string{}
	imdb := strings.TrimSpace(info.ExternalIDs.IMDB)
	if imdb != "" {
		typeParam := "movie"
		if wItem.Type == "series" {
			typeParam = "tv"
		}
		trs, _ := movies.GetTorrentsByIMDB(ctx, imdb, typeParam)
		voices = neomovies.UniqueVoices(trs)
	}

	return libraryItem{
		KPID:          wItem.KPID,
		Type:          wItem.Type,
		Title:         title,
		PosterURL:     posterURL,
		Rating:        rating,
		Overview:      overview,
		Genres:        genres,
		SeasonsCount:  seasonsCount,
		EpisodesCount: episodesCount,
		Voices:        voices,
	}, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func handleInlineQuery(ctx context.Context, w http.ResponseWriter, bot *tg.Client, movies *neomovies.Client, db *storage.Mongo, q *inlineQuery) {
	query := strings.TrimSpace(q.Query)
	switch strings.ToLower(query) {
	case "#movies", "movies":
		query = "#movies"
	case "#tv", "tv", "series":
		query = "#tv"
	}
	if query == "" {
		res, err := movies.GetPopular(ctx, 1)
		if err != nil {
			log.Printf("inline popular error: %v", err)
			_ = bot.AnswerInlineQuery(ctx, tg.AnswerInlineQueryRequest{InlineQueryID: q.ID, Results: []tg.InlineQueryResult{}, CacheTime: 1, IsPersonal: true})
			w.WriteHeader(http.StatusOK)
			return
		}
		results := buildInlineResults(ctx, movies, db, res)
		if err := bot.AnswerInlineQuery(ctx, tg.AnswerInlineQueryRequest{InlineQueryID: q.ID, Results: results, CacheTime: 5, IsPersonal: true}); err != nil {
			log.Printf("inline popular answer error: %v (results=%d)", err, len(results))
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	res, err := movies.SearchMovies(ctx, query, 1)
	if err != nil {
		log.Printf("inline search error: %v (query=%q)", err, query)
		_ = bot.AnswerInlineQuery(ctx, tg.AnswerInlineQueryRequest{InlineQueryID: q.ID, Results: []tg.InlineQueryResult{}, CacheTime: 1, IsPersonal: true})
		w.WriteHeader(http.StatusOK)
		return
	}

	results := buildInlineResults(ctx, movies, db, res)
	if err := bot.AnswerInlineQuery(ctx, tg.AnswerInlineQueryRequest{InlineQueryID: q.ID, Results: results, CacheTime: 5, IsPersonal: true}); err != nil {
		log.Printf("inline search answer error: %v (query=%q results=%d)", err, query, len(results))
	}
	w.WriteHeader(http.StatusOK)
}

func handleChosenInline(ctx context.Context, w http.ResponseWriter, bot *tg.Client, movies *neomovies.Client, db *storage.Mongo, chosen *chosenInline) {
	kpID, err := strconv.Atoi(strings.TrimSpace(chosen.ResultID))
	if err != nil || kpID <= 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	payload, err := buildMoviePayload(ctx, movies, db, kpID)
	if err != nil {
		log.Printf("chosen inline payload error: %v (kp_id=%d)", err, kpID)
		w.WriteHeader(http.StatusOK)
		return
	}

	if chosen.InlineMessageID != "" {
		if err := bot.EditMessageMedia(ctx, tg.EditMessageMediaRequest{
			InlineMessageID: chosen.InlineMessageID,
			Media: tg.InputMediaPhoto{
				Type:      "photo",
				Media:     payload.PhotoURL,
				Caption:   payload.Caption,
				ParseMode: "HTML",
			},
			ReplyMarkup: &payload.Keyboard,
		}); err != nil {
			log.Printf("chosen inline editMessageMedia error: %v (kp_id=%d)", err, kpID)
		}
	} else {
		if err := bot.SendPhoto(ctx, tg.SendPhotoRequest{
			ChatID:      chosen.From.ID,
			Photo:       payload.PhotoURL,
			Caption:     payload.Caption,
			ParseMode:   "HTML",
			ReplyMarkup: &payload.Keyboard,
		}); err != nil {
			log.Printf("chosen inline sendPhoto error: %v (kp_id=%d)", err, kpID)
		}
	}

	w.WriteHeader(http.StatusOK)
}

func buildInlineResults(ctx context.Context, movies *neomovies.Client, db *storage.Mongo, res *neomovies.SearchResponse) []tg.InlineQueryResult {
	results := make([]tg.InlineQueryResult, 0, 10)
	for i, m := range res.Results {
		if i >= 10 {
			break
		}

		kpID := m.ExternalIDs.KP
		if kpID == 0 {
			kpID = m.KinopoiskID
		}
		if kpID == 0 {
			continue
		}

		inlineCache.Set(kpID, inlineMovieData{
			KPID:             kpID,
			Title:            firstNonEmpty(m.Title, m.NameRu, m.Name, m.NameOriginal),
			Year:             m.Year,
			Overview:         m.Overview,
			Description:      m.Description,
			ShortDescription: m.ShortDescription,
			PosterPath:       m.PosterPath,
			PosterURL:        m.PosterURL,
			PosterURLPreview: m.PosterURLPreview,
			Rating:           m.Rating,
			RatingKinopoisk:  m.RatingKinopoisk,
			VoteAverage:      m.VoteAverage,
			Genres:           m.Genres,
		}, 30*time.Minute)

		title := firstNonEmpty(m.Title, m.NameRu, m.Name, m.NameOriginal)
		year := ""
		if m.Year != "" {
			year = m.Year
		} else if m.ReleaseDate != "" {
			year = m.ReleaseDate[0:4]
		}
		displayTitle := title
		if year != "" {
			displayTitle = fmt.Sprintf("%s (%s)", title, year)
		}

		rating := m.Rating
		if rating == 0 {
			rating = m.RatingKinopoisk
		}
		if rating == 0 {
			rating = m.VoteAverage
		}

		desc := strings.TrimSpace(firstNonEmpty(m.Overview, m.Description, m.ShortDescription))
		if len([]rune(desc)) > 900 {
			desc = string([]rune(desc)[:900]) + "…"
		}

		poster := firstNonEmpty(m.PosterPath, m.PosterURLPreview, m.PosterURL)
		thumbURL := movies.ImageURL(poster, "kp_small", kpID)

		caption := html.EscapeString(displayTitle)
		if rating > 0 {
			caption = fmt.Sprintf("%s\nКинопоиск: %.1f", caption, rating)
		}
		if desc != "" {
			caption = caption + "\n\n" + html.EscapeString(desc)
		}
		caption = truncateRunes(caption, 950)

		descLines := make([]string, 0, 2)
		if rating > 0 {
			descLines = append(descLines, fmt.Sprintf("Кинопоиск: %.1f", rating))
		}
		if len(m.Genres) > 0 {
			genres := make([]string, 0, 3)
			for _, g := range m.Genres {
				name := strings.TrimSpace(g.Name)
				if name != "" {
					genres = append(genres, name)
				}
				if len(genres) >= 3 {
					break
				}
			}
			if len(genres) > 0 {
				descLines = append(descLines, strings.Join(genres, ", "))
			}
		}
		description := truncateRunes(strings.Join(descLines, " • "), 180)

		messageText := fmt.Sprintf("/get %d", kpID)

		result := tg.InlineQueryResultArticle{
			Type:  "article",
			ID:    strconv.FormatInt(int64(kpID), 10),
			Title: displayTitle,
			InputMessageContent: tg.InputTextMessageContent{
				MessageText: messageText,
				ParseMode:   "HTML",
			},
			Description: description,
		}
		if thumbURL != "" {
			result.ThumbURL = thumbURL
			result.ThumbWidth = 80
			result.ThumbHeight = 120
		}
		results = append(results, result)
	}
	return results
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}

func handleCallback(ctx context.Context, w http.ResponseWriter, bot *tg.Client, movies *neomovies.Client, db *storage.Mongo, cq *callbackQuery) {
	data := strings.TrimSpace(cq.Data)
	if data == "close" {
		if cq.Message != nil {
			_ = bot.DeleteMessage(ctx, cq.Message.Chat.ID, cq.Message.MessageID)
		} else if cq.InlineMessageID != "" {
			_ = bot.EditMessageReplyMarkup(ctx, tg.EditMessageReplyMarkupRequest{InlineMessageID: cq.InlineMessageID, ReplyMarkup: &tg.InlineKeyboardMarkup{InlineKeyboard: [][]tg.InlineKeyboardButton{}}})
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if data == "menu:new" || data == "menu:movies" || data == "menu:series" {
		if cq.Message != nil {
			query := ""
			text := "Открой поиск и набери название — я покажу карточки.\n\nПодсказка: можно нажать кнопку “Поиск” в меню."
			if data == "menu:movies" {
				query = "#movies"
				text = "Топ фильмов. Нажми кнопку ниже."
			} else if data == "menu:series" {
				query = "#tv"
				text = "Топ сериалов. Нажми кнопку ниже."
			}
			var kb *tg.InlineKeyboardMarkup
			if query != "" {
				markup := tg.NewInlineKeyboardMarkup([][]tg.InlineKeyboardButton{
					{{Text: "Показать", SwitchInlineQueryCurrentChat: tg.StrPtr(query)}},
				})
				kb = &markup
			}
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: cq.Message.Chat.ID, Text: text, ReplyMarkup: kb})
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(data, "watch:") {
		kpStr := strings.TrimPrefix(data, "watch:")
		kpID, _ := strconv.Atoi(kpStr)
		if kpID <= 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}

		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "Нет в Telegram")
			w.WriteHeader(http.StatusOK)
			return
		}

		if cq.Message != nil {
			if item.Type == "movie" {
				copiedID, err := bot.CopyMessage(ctx, cq.Message.Chat.ID, item.StorageChatID, item.StorageMessageID)
				if err == nil && copiedID > 0 {
					closeKB := tg.NewInlineKeyboardMarkup([][]tg.InlineKeyboardButton{
						{{Text: "Закрыть", CallbackData: "close"}},
					})
					_ = bot.EditMessageReplyMarkup(ctx, tg.EditMessageReplyMarkupRequest{
						ChatID:      cq.Message.Chat.ID,
						MessageID:   copiedID,
						ReplyMarkup: &closeKB,
					})
				}
			} else if item.Type == "series" {
				title := strings.TrimSpace(item.Title)
				if title == "" {
					title = fmt.Sprintf("kp_%d", item.KPID)
				}
				_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: cq.Message.Chat.ID, Text: title, ReplyMarkup: item.SeriesKeyboard()})
			}
		}

		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(data, "season:") {
		parts := strings.Split(data, ":")
		if len(parts) != 3 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		if kpID <= 0 || seasonNum <= 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		if cq.Message != nil {
			title := strings.TrimSpace(item.Title)
			if title == "" {
				title = fmt.Sprintf("kp_%d", item.KPID)
			}
			text := fmt.Sprintf("%s\nСезон %d", title, seasonNum)
			_ = bot.EditMessageText(ctx, tg.EditMessageTextRequest{
				ChatID:      cq.Message.Chat.ID,
				MessageID:   cq.Message.MessageID,
				Text:        text,
				ReplyMarkup: item.SeasonKeyboard(seasonNum, 1),
			})
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(data, "seasonpage:") {
		parts := strings.Split(data, ":")
		if len(parts) != 4 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		pageNum, _ := strconv.Atoi(parts[3])
		if kpID <= 0 || seasonNum <= 0 || pageNum <= 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		if cq.Message != nil {
			_ = bot.EditMessageReplyMarkup(ctx, tg.EditMessageReplyMarkupRequest{
				ChatID:      cq.Message.Chat.ID,
				MessageID:   cq.Message.MessageID,
				ReplyMarkup: item.SeasonKeyboard(seasonNum, pageNum),
			})
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(data, "ep:") {
		parts := strings.Split(data, ":")
		if len(parts) != 4 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		epNum, _ := strconv.Atoi(parts[3])
		if kpID <= 0 || seasonNum <= 0 || epNum <= 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		var chatID int64
		if cq.Message != nil {
			chatID = cq.Message.Chat.ID
		}
		if chatID == 0 {
			_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
			w.WriteHeader(http.StatusOK)
			return
		}
		var ep *storage.Episode
		for si := range item.Seasons {
			if item.Seasons[si].Number != seasonNum {
				continue
			}
			for ei := range item.Seasons[si].Episodes {
				if item.Seasons[si].Episodes[ei].Number == epNum {
					ep = &item.Seasons[si].Episodes[ei]
					break
				}
			}
		}
		if ep != nil {
			copiedID, err := bot.CopyMessage(ctx, chatID, ep.StorageChatID, ep.StorageMessageID)
			if err == nil && copiedID > 0 {
				closeKB := tg.NewInlineKeyboardMarkup([][]tg.InlineKeyboardButton{
					{{Text: "Закрыть", CallbackData: "close"}},
				})
				_ = bot.EditMessageReplyMarkup(ctx, tg.EditMessageReplyMarkupRequest{
					ChatID:      chatID,
					MessageID:   copiedID,
					ReplyMarkup: &closeKB,
				})
			}
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}

	_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
	w.WriteHeader(http.StatusOK)
}

func handleMessage(ctx context.Context, w http.ResponseWriter, bot *tg.Client, movies *neomovies.Client, db *storage.Mongo, msg *message) {
	text := strings.TrimSpace(msg.Text)
	log.Printf("message received chat_id=%d text=%q", msg.Chat.ID, text)
	if strings.HasPrefix(text, "/start") {
		log.Printf("/start from chat_id=%d", msg.Chat.ID)
		parts := strings.Fields(text)
		if len(parts) > 1 {
			payload := strings.TrimSpace(parts[1])
			if strings.HasPrefix(payload, "get_") {
				idStr := strings.TrimPrefix(payload, "get_")
				if kpID, _ := strconv.Atoi(idStr); kpID > 0 {
					if err := sendMovieCard(ctx, bot, movies, db, msg.Chat.ID, kpID); err != nil {
						log.Printf("start get error: %v (kp_id=%d)", err, kpID)
					}
					w.WriteHeader(http.StatusOK)
					return
				}
			}
		}
		kb := tg.NewInlineKeyboardMarkup([][]tg.InlineKeyboardButton{
			{{Text: "Фильмы", CallbackData: "menu:movies"}, {Text: "Сериалы", CallbackData: "menu:series"}},
			{{Text: "Поиск", SwitchInlineQueryCurrentChat: tg.StrPtr("")}},
			{{Text: "Кинотека в боте", URL: "https://tg.neomovies.ru/"}},
			{{Text: "Закрыть", CallbackData: "close"}},
		})
		if err := bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Это библиотека кино и сериалов с быстрым поиском.\n\nНажми “Поиск” и введи название — я покажу карточки.\n\nЕсли просто написать @neomovies_tg_bot без текста — покажу популярное.", ReplyMarkup: &kb}); err != nil {
			log.Printf("/start sendMessage error: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/get ") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		if kpID <= 0 {
			w.WriteHeader(http.StatusOK)
			return
		}
		if err := sendMovieCard(ctx, bot, movies, db, msg.Chat.ID, kpID); err != nil {
			log.Printf("public /get error: %v (kp_id=%d)", err, kpID)
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if text == "/help" || text == "help" {
		adminChatIDStr := strings.TrimSpace(os.Getenv("ADMIN_CHAT_ID"))
		if adminChatIDStr == "" {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("ADMIN_CHAT_ID не задан. Твой chat_id=%d", msg.Chat.ID)})
			w.WriteHeader(http.StatusOK)
			return
		}
		adminChatID, err := strconv.ParseInt(adminChatIDStr, 10, 64)
		if err != nil || adminChatID == 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("ADMIN_CHAT_ID неверный. Твой chat_id=%d", msg.Chat.ID)})
			w.WriteHeader(http.StatusOK)
			return
		}
		if msg.Chat.ID != adminChatID {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("Нет доступа. Твой chat_id=%d", msg.Chat.ID)})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "/help\n\n/addmovie <kp_id> <storage_chat_id> <storage_message_id>\n/addmovie <kp_id>   (reply to forwarded channel post)\n\n/addseries <kp_id> <title>\n\n/addepisode <kp_id> <season> <episode> <storage_chat_id> <storage_message_id>\n/addepisode <kp_id> <season> <episode>   (reply to forwarded channel post)\n\n/getinfo <kp_id>\n/del <kp_id>\n/list [limit]"})
		w.WriteHeader(http.StatusOK)
		return
	}

	adminChatIDStr := strings.TrimSpace(os.Getenv("ADMIN_CHAT_ID"))
	if adminChatIDStr == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	adminChatID, err := strconv.ParseInt(adminChatIDStr, 10, 64)
	if err != nil || adminChatID == 0 {
		if text == "/help" || text == "help" {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("ADMIN_CHAT_ID неверный. Твой chat_id=%d", msg.Chat.ID)})
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if msg.Chat.ID != adminChatID {
		if text == "/help" || text == "help" {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: fmt.Sprintf("Нет доступа. Твой chat_id=%d", msg.Chat.ID)})
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if strings.HasPrefix(text, "/addmovie ") {
		parts := strings.Fields(text)
		if len(parts) != 2 && len(parts) != 4 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /addmovie <kp_id> <storage_chat_id> <storage_message_id> OR reply to forwarded post: /addmovie <kp_id>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		var storageChatID int64
		var storageMsgID int
		if len(parts) == 4 {
			storageChatID, _ = strconv.ParseInt(parts[2], 10, 64)
			storageMsgID, _ = strconv.Atoi(parts[3])
		} else {
			if msg.ReplyToMessage == nil || msg.ReplyToMessage.ForwardFromChat == nil || msg.ReplyToMessage.ForwardFromMessageID == 0 {
				_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Reply to a forwarded post from the storage channel."})
				w.WriteHeader(http.StatusOK)
				return
			}
			storageChatID = msg.ReplyToMessage.ForwardFromChat.ID
			storageMsgID = msg.ReplyToMessage.ForwardFromMessageID
		}
		if kpID <= 0 || storageChatID == 0 || storageMsgID <= 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid args"})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = db.UpsertWatchMovie(ctx, kpID, storageChatID, storageMsgID)
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK"})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/addseries ") {
		rest := strings.TrimSpace(strings.TrimPrefix(text, "/addseries"))
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /addseries <kp_id> <title>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(fields[0])
		title := strings.TrimSpace(rest[len(fields[0]):])
		if kpID <= 0 || title == "" {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid args"})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = db.UpsertWatchSeries(ctx, kpID, title)
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK"})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/addepisode ") {
		parts := strings.Fields(text)
		if len(parts) != 4 && len(parts) != 6 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /addepisode <kp_id> <season> <episode> <storage_chat_id> <storage_message_id> OR reply to forwarded post: /addepisode <kp_id> <season> <episode>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		seasonNum, _ := strconv.Atoi(parts[2])
		epNum, _ := strconv.Atoi(parts[3])
		var storageChatID int64
		var storageMsgID int
		if len(parts) == 6 {
			storageChatID, _ = strconv.ParseInt(parts[4], 10, 64)
			storageMsgID, _ = strconv.Atoi(parts[5])
		} else {
			if msg.ReplyToMessage == nil || msg.ReplyToMessage.ForwardFromChat == nil || msg.ReplyToMessage.ForwardFromMessageID == 0 {
				_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Reply to a forwarded post from the storage channel."})
				w.WriteHeader(http.StatusOK)
				return
			}
			storageChatID = msg.ReplyToMessage.ForwardFromChat.ID
			storageMsgID = msg.ReplyToMessage.ForwardFromMessageID
		}
		if kpID <= 0 || seasonNum <= 0 || epNum <= 0 || storageChatID == 0 || storageMsgID <= 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid args"})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = db.UpsertSeriesEpisode(ctx, kpID, seasonNum, epNum, storageChatID, storageMsgID)
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK"})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/getinfo ") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /get <kp_id>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		if kpID <= 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid kp_id"})
			w.WriteHeader(http.StatusOK)
			return
		}
		item, _ := db.GetWatchItemByKPID(ctx, kpID)
		if item == nil {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Not found"})
			w.WriteHeader(http.StatusOK)
			return
		}
		textOut := fmt.Sprintf("kp_id=%d\ntype=%s\ntitle=%s\nmovie_ref=%d:%d\nseasons=%d", item.KPID, item.Type, item.Title, item.StorageChatID, item.StorageMessageID, len(item.Seasons))
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: textOut})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/del ") {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Usage: /del <kp_id>"})
			w.WriteHeader(http.StatusOK)
			return
		}
		kpID, _ := strconv.Atoi(parts[1])
		if kpID <= 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Invalid kp_id"})
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = db.DeleteByKPID(ctx, kpID)
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "OK"})
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.HasPrefix(text, "/list") {
		parts := strings.Fields(text)
		limit := 20
		if len(parts) >= 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil {
				limit = n
			}
		}
		items, err := db.ListRecent(ctx, limit)
		if err != nil {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "DB not configured"})
			w.WriteHeader(http.StatusOK)
			return
		}
		if len(items) == 0 {
			_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "Empty"})
			w.WriteHeader(http.StatusOK)
			return
		}
		b := strings.Builder{}
		for _, it := range items {
			name := strings.TrimSpace(it.Title)
			if name == "" {
				name = fmt.Sprintf("kp_%d", it.KPID)
			}
			b.WriteString(fmt.Sprintf("%d %s %s\n", it.KPID, it.Type, name))
		}
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: strings.TrimSpace(b.String())})
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
}

var iframeSrcRe = regexp.MustCompile(`(?i)src=\"([^\"]+)\"`)
var iframeDataSrcRe = regexp.MustCompile(`(?i)data-src=\"([^\"]+)\"`)

func playerHandler(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	idType := strings.TrimSpace(r.URL.Query().Get("idType"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if provider == "" || idType == "" || id == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	apiBase := strings.TrimRight(os.Getenv("API_BASE"), "/")
	if apiBase == "" {
		apiBase = "https://api.neomovies.ru"
	}

	u := fmt.Sprintf("%s/api/v1/players/%s/%s/%s", apiBase, url.PathEscape(provider), url.PathEscape(idType), url.PathEscape(id))

	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	content := strings.TrimSpace(string(b))
	if strings.HasPrefix(content, "<") {
		if m := iframeSrcRe.FindStringSubmatch(content); len(m) == 2 {
			http.Redirect(w, r, m[1], http.StatusFound)
			return
		}
		if m := iframeDataSrcRe.FindStringSubmatch(content); len(m) == 2 {
			http.Redirect(w, r, m[1], http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
		return
	}

	http.Redirect(w, r, content, http.StatusFound)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

type moviePayload struct {
	PhotoURL string
	Caption  string
	Keyboard tg.InlineKeyboardMarkup
}

type inlineMovieData struct {
	KPID             int
	Title            string
	Year             string
	Overview         string
	Description      string
	ShortDescription string
	PosterPath       string
	PosterURL        string
	PosterURLPreview string
	Rating           float64
	RatingKinopoisk  float64
	VoteAverage      float64
	Genres           []neomovies.MovieGenre
}

type inlineCacheEntry struct {
	data    inlineMovieData
	expires time.Time
}

type inlineMovieCache struct {
	mu    sync.Mutex
	items map[int]inlineCacheEntry
}

func newInlineMovieCache() *inlineMovieCache {
	return &inlineMovieCache{items: map[int]inlineCacheEntry{}}
}

func (c *inlineMovieCache) Get(kpID int) (inlineMovieData, bool) {
	if kpID <= 0 {
		return inlineMovieData{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[kpID]
	if !ok {
		return inlineMovieData{}, false
	}
	if time.Now().After(entry.expires) {
		delete(c.items, kpID)
		return inlineMovieData{}, false
	}
	return entry.data, true
}

func (c *inlineMovieCache) Set(kpID int, data inlineMovieData, ttl time.Duration) {
	if kpID <= 0 {
		return
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	c.mu.Lock()
	c.items[kpID] = inlineCacheEntry{data: data, expires: time.Now().Add(ttl)}
	c.mu.Unlock()
}

var inlineCache = newInlineMovieCache()

func buildMoviePayload(ctx context.Context, movies *neomovies.Client, db *storage.Mongo, kpID int) (*moviePayload, error) {
	if kpID <= 0 {
		return nil, fmt.Errorf("invalid kp_id")
	}

	log.Printf("movie payload: kp_id=%d", kpID)

	apiBase := strings.TrimRight(os.Getenv("API_BASE"), "/")
	if apiBase == "" {
		apiBase = "https://api.neomovies.ru"
	}

	info, err := movies.GetMovieByKPID(ctx, kpID)
	if err != nil || info == nil {
		log.Printf("movie payload: GetMovieByKPID failed kp_id=%d err=%v", kpID, err)
		return nil, fmt.Errorf("get movie failed: %v", err)
	}
	log.Printf("movie payload: GetMovieByKPID ok kp_id=%d title=%q nameRu=%q name=%q year=%q release=%q rating=%.2f kp=%.2f vote=%.2f poster=%q", kpID, info.Title, info.NameRu, info.Name, info.Year, info.ReleaseDate, info.Rating, info.RatingKinopoisk, info.VoteAverage, info.PosterPath)

	title := strings.TrimSpace(firstNonEmpty(info.Title, info.NameRu, info.Name, info.NameOriginal))
	if cached, ok := inlineCache.Get(kpID); ok {
		if title == "" {
			title = strings.TrimSpace(firstNonEmpty(cached.Title))
		}
		if info.Overview == "" && info.Description == "" && info.ShortDescription == "" {
			info.Overview = cached.Overview
			info.Description = cached.Description
			info.ShortDescription = cached.ShortDescription
		}
		if len(info.Genres) == 0 && len(cached.Genres) > 0 {
			info.Genres = cached.Genres
		}
		if info.Rating == 0 {
			info.Rating = cached.Rating
		}
		if info.RatingKinopoisk == 0 {
			info.RatingKinopoisk = cached.RatingKinopoisk
		}
		if info.VoteAverage == 0 {
			info.VoteAverage = cached.VoteAverage
		}
		if info.Year == "" {
			info.Year = cached.Year
		}
		if info.ReleaseDate == "" {
			info.ReleaseDate = cached.Year
		}
		if info.PosterPath == "" && cached.PosterPath != "" {
			info.PosterPath = cached.PosterPath
		}
		if info.PosterURL == "" && cached.PosterURL != "" {
			info.PosterURL = cached.PosterURL
		}
		if info.PosterURLPreview == "" && cached.PosterURLPreview != "" {
			info.PosterURLPreview = cached.PosterURLPreview
		}
	}
	if title == "" && db != nil {
		if item, _ := db.GetWatchItemByKPID(ctx, kpID); item != nil {
			title = strings.TrimSpace(item.Title)
		}
	}
	if title == "" || (info.Overview == "" && info.Description == "" && info.ShortDescription == "") {
		log.Printf("movie payload: fallback search kp_id=%d", kpID)
		if sr, _ := movies.SearchMovies(ctx, strconv.Itoa(kpID), 1); sr != nil {
			log.Printf("movie payload: search results kp_id=%d count=%d", kpID, len(sr.Results))
			for _, m := range sr.Results {
				matchID := m.ExternalIDs.KP
				if matchID == 0 {
					matchID = m.KinopoiskID
				}
				if matchID != kpID {
					continue
				}
				log.Printf("movie payload: search matched kp_id=%d title=%q nameRu=%q year=%q rating=%.2f kp=%.2f vote=%.2f", kpID, m.Title, m.NameRu, m.Year, m.Rating, m.RatingKinopoisk, m.VoteAverage)
				if title == "" {
					title = strings.TrimSpace(firstNonEmpty(m.Title, m.NameRu, m.Name, m.NameOriginal))
				}
				if info.Overview == "" && info.Description == "" && info.ShortDescription == "" {
					info.Overview = m.Overview
					info.Description = m.Description
					info.ShortDescription = m.ShortDescription
				}
				if len(info.Genres) == 0 && len(m.Genres) > 0 {
					info.Genres = m.Genres
				}
				if info.Rating == 0 && m.Rating > 0 {
					info.Rating = m.Rating
				}
				if info.RatingKinopoisk == 0 && m.RatingKinopoisk > 0 {
					info.RatingKinopoisk = m.RatingKinopoisk
				}
				if info.VoteAverage == 0 && m.VoteAverage > 0 {
					info.VoteAverage = m.VoteAverage
				}
				if info.Year == "" && m.Year != "" {
					info.Year = m.Year
				}
				if info.ReleaseDate == "" && m.ReleaseDate != "" {
					info.ReleaseDate = m.ReleaseDate
				}
				break
			}
		}
	}

	if title == "" {
		title = fmt.Sprintf("kp_%d", kpID)
	}
	year := ""
	if info.Year != "" {
		year = info.Year
	} else if info.ReleaseDate != "" && len(info.ReleaseDate) >= 4 {
		year = info.ReleaseDate[0:4]
	}
	displayTitle := title
	if year != "" {
		displayTitle = fmt.Sprintf("%s (%s)", title, year)
	}

	rating := info.Rating
	if rating == 0 {
		rating = info.RatingKinopoisk
	}
	if rating == 0 {
		rating = info.VoteAverage
	}

	desc := strings.TrimSpace(firstNonEmpty(info.Overview, info.Description, info.ShortDescription))
	if len([]rune(desc)) > 900 {
		desc = string([]rune(desc)[:900]) + "…"
	}

	photoURL := fmt.Sprintf("%s/api/v1/images/kp/%d", apiBase, kpID)
	log.Printf("movie payload: kp_id=%d photoURL=%q poster=%q desc_len=%d", kpID, photoURL, info.PosterPath, len([]rune(desc)))

	keyboard := tg.NewInlineKeyboardMarkup([][]tg.InlineKeyboardButton{
		{
			{Text: "Плеер 1 (Collaps)", URL: movies.PlayerRedirectURL("collaps", "kp", kpID)},
			{Text: "Плеер 2 (Lumex)", URL: movies.PlayerRedirectURL("lumex", "kp", kpID)},
		},
	})
	if db != nil {
		if watch, _ := db.GetWatchItemByKPID(ctx, kpID); watch != nil {
			keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []tg.InlineKeyboardButton{{Text: "Смотреть в Telegram", CallbackData: fmt.Sprintf("watch:%d", kpID)}})
		}
	}
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []tg.InlineKeyboardButton{{Text: "Закрыть", CallbackData: "close"}})

	captionLines := []string{fmt.Sprintf("<b>%s</b>", html.EscapeString(displayTitle))}
	if rating > 0 {
		captionLines = append(captionLines, fmt.Sprintf("<b>Кинопоиск</b>: %.1f", rating))
	}
	if len(info.Genres) > 0 {
		genres := make([]string, 0, 4)
		for _, g := range info.Genres {
			name := strings.TrimSpace(g.Name)
			if name != "" {
				genres = append(genres, name)
			}
			if len(genres) >= 4 {
				break
			}
		}
		if len(genres) > 0 {
			captionLines = append(captionLines, fmt.Sprintf("<b>%s</b>", html.EscapeString(strings.Join(genres, ", "))))
		}
	}
	caption := strings.Join(captionLines, "\n")
	if desc != "" {
		caption = caption + "\n\n" + html.EscapeString("«"+desc+"»")
	}
	caption = truncateRunes(caption, 950)
	if strings.TrimSpace(caption) == "" {
		caption = html.EscapeString(fmt.Sprintf("kp_%d", kpID))
	}

	return &moviePayload{
		PhotoURL: photoURL,
		Caption:  caption,
		Keyboard: keyboard,
	}, nil
}

func sendMovieCard(ctx context.Context, bot *tg.Client, movies *neomovies.Client, db *storage.Mongo, chatID int64, kpID int) error {
	if kpID <= 0 {
		return fmt.Errorf("invalid kp_id")
	}

	payload, err := buildMoviePayload(ctx, movies, db, kpID)
	if err != nil {
		return err
	}
	if payload.PhotoURL != "" {
		if err := bot.SendPhoto(ctx, tg.SendPhotoRequest{
			ChatID:      chatID,
			Photo:       payload.PhotoURL,
			Caption:     payload.Caption,
			ParseMode:   "HTML",
			ReplyMarkup: &payload.Keyboard,
		}); err == nil {
			return nil
		}
	}

	return bot.SendMessage(ctx, tg.SendMessageRequest{
		ChatID:      chatID,
		Text:        payload.Caption,
		ParseMode:   "HTML",
		ReplyMarkup: &payload.Keyboard,
	})
}

func stripHTMLTags(s string) string {
	out := strings.Builder{}
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(out.String())
}
