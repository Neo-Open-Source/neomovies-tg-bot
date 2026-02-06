package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"neomovies-tg-bot/internal/neomovies"
	"neomovies-tg-bot/internal/storage"
	"neomovies-tg-bot/internal/tg"
)

type update struct {
	UpdateID      int             `json:"update_id"`
	InlineQuery   *inlineQuery    `json:"inline_query"`
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

type callbackQuery struct {
	ID      string   `json:"id"`
	From    user     `json:"from"`
	Data    string   `json:"data"`
	Message *message `json:"message"`
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
	case upd.CallbackQuery != nil:
		handleCallback(ctx, w, bot, movies, db, upd.CallbackQuery)
		return
	case upd.Message != nil:
		handleMessage(ctx, w, bot, db, upd.Message)
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

	title := strings.TrimSpace(firstNonEmpty(info.Title, info.NameRu, info.Name, info.NameOriginal, wItem.Title))
	poster := firstNonEmpty(info.PosterPath, info.PosterURLPreview, info.PosterURL)
	posterURL := movies.ImageURL(poster, "kp_big", wItem.KPID)
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
	if query == "" {
		_ = bot.AnswerInlineQuery(ctx, tg.AnswerInlineQueryRequest{InlineQueryID: q.ID, Results: []tg.InlineQueryResult{}, CacheTime: 1, IsPersonal: true})
		w.WriteHeader(http.StatusOK)
		return
	}

	res, err := movies.SearchMovies(ctx, query, 1)
	if err != nil {
		_ = bot.AnswerInlineQuery(ctx, tg.AnswerInlineQueryRequest{InlineQueryID: q.ID, Results: []tg.InlineQueryResult{}, CacheTime: 1, IsPersonal: true})
		w.WriteHeader(http.StatusOK)
		return
	}

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
		photoURL := movies.ImageURL(poster, "kp_big", kpID)
		thumbURL := movies.ImageURL(poster, "kp_small", kpID)

		watch, _ := db.GetWatchItemByKPID(ctx, kpID)
		keyboard := tg.NewInlineKeyboardMarkup([][]tg.InlineKeyboardButton{
			{
				{Text: "Плеер 1 (Collaps)", URL: movies.PlayerRedirectURL("collaps", "kp", kpID)},
				{Text: "Плеер 2 (Lumex)", URL: movies.PlayerRedirectURL("lumex", "kp", kpID)},
			},
		})
		if watch != nil {
			keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []tg.InlineKeyboardButton{{Text: "Смотреть в Telegram", CallbackData: fmt.Sprintf("watch:%d", kpID)}})
		}
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []tg.InlineKeyboardButton{{Text: "Закрыть", CallbackData: "close"}})

		caption := displayTitle
		if rating > 0 {
			caption = fmt.Sprintf("%s\nКинопоиск: %.1f", caption, rating)
		}
		if desc != "" {
			caption = caption + "\n\n" + desc
		}

		results = append(results, tg.InlineQueryResultPhoto{
			Type:        "photo",
			ID:          strconv.FormatInt(int64(kpID), 10),
			PhotoURL:    photoURL,
			ThumbURL:    thumbURL,
			Caption:     caption,
			ParseMode:   "HTML",
			ReplyMarkup: &keyboard,
		})
	}

	_ = bot.AnswerInlineQuery(ctx, tg.AnswerInlineQueryRequest{InlineQueryID: q.ID, Results: results, CacheTime: 5, IsPersonal: true})
	w.WriteHeader(http.StatusOK)
}

func handleCallback(ctx context.Context, w http.ResponseWriter, bot *tg.Client, movies *neomovies.Client, db *storage.Mongo, cq *callbackQuery) {
	data := strings.TrimSpace(cq.Data)
	if data == "close" {
		if cq.Message != nil {
			_ = bot.DeleteMessage(ctx, cq.Message.Chat.ID, cq.Message.MessageID)
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
				_ = bot.CopyMessage(ctx, cq.Message.Chat.ID, item.StorageChatID, item.StorageMessageID)
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
			text := strings.TrimSpace(cq.Message.Text)
			if text == "" {
				text = strings.TrimSpace(item.Title)
				if text == "" {
					text = fmt.Sprintf("kp_%d", item.KPID)
				}
			}
			_ = bot.EditMessageText(ctx, tg.EditMessageTextRequest{ChatID: cq.Message.Chat.ID, MessageID: cq.Message.MessageID, Text: text, ReplyMarkup: item.SeasonKeyboard(seasonNum)})
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
			_ = bot.CopyMessage(ctx, chatID, ep.StorageChatID, ep.StorageMessageID)
		}
		_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
		w.WriteHeader(http.StatusOK)
		return
	}

	_ = bot.AnswerCallbackQuery(ctx, cq.ID, "")
	w.WriteHeader(http.StatusOK)
}

func handleMessage(ctx context.Context, w http.ResponseWriter, bot *tg.Client, db *storage.Mongo, msg *message) {
	adminChatIDStr := strings.TrimSpace(os.Getenv("ADMIN_CHAT_ID"))
	if adminChatIDStr == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	adminChatID, err := strconv.ParseInt(adminChatIDStr, 10, 64)
	if err != nil || adminChatID == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}
	if msg.Chat.ID != adminChatID {
		w.WriteHeader(http.StatusOK)
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "/help" || text == "help" {
		_ = bot.SendMessage(ctx, tg.SendMessageRequest{ChatID: msg.Chat.ID, Text: "/help\n\n/addmovie <kp_id> <storage_chat_id> <storage_message_id>\n/addmovie <kp_id>   (reply to forwarded channel post)\n\n/addseries <kp_id> <title>\n\n/addepisode <kp_id> <season> <episode> <storage_chat_id> <storage_message_id>\n/addepisode <kp_id> <season> <episode>   (reply to forwarded channel post)\n\n/get <kp_id>\n/del <kp_id>\n/list [limit]"})
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
	if strings.HasPrefix(text, "/get ") {
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}
