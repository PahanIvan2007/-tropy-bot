package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "github.com/lib/pq"
)

var (
	db       *sql.DB
	bot      *tgbotapi.BotAPI
	cache    = &TTLCache{data: make(map[string]cacheEntry)}
	botToken string
	gameURL  string
)

const ttl = 15 * time.Second

type cacheEntry struct {
	data    interface{}
	expires time.Time
}

type TTLCache struct {
	mu   sync.RWMutex
	data map[string]cacheEntry
}

func (c *TTLCache) Get(key string) interface{} {
	c.mu.RLock()
	e, ok := c.data[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expires) {
		return nil
	}
	return e.data
}

func (c *TTLCache) Set(key string, val interface{}) {
	c.mu.Lock()
	c.data[key] = cacheEntry{val, time.Now().Add(ttl)}
	c.mu.Unlock()
}

func queryCache(key, sqlStr string, args ...interface{}) ([]map[string]interface{}, error) {
	if v := cache.Get(key); v != nil {
		return v.([]map[string]interface{}), nil
	}
	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var result []map[string]interface{}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		valPtrs := make([]interface{}, len(cols))
		for i := range vals {
			valPtrs[i] = &vals[i]
		}
		rows.Scan(valPtrs...)
		row := make(map[string]interface{})
		for i, c := range cols {
			row[c] = vals[i]
		}
		result = append(result, row)
	}
	cache.Set(key, result)
	return result, nil
}

func main() {
	botToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN not set")
	}
	gameURL = os.Getenv("GAME_URL")
	if gameURL == "" {
		gameURL = "http://localhost:3000/game/"
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:123@localhost:5432/water_sports_platform?sslmode=disable"
	}

	var err error
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal("DB open:", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err = db.Ping(); err != nil {
		log.Fatal("DB ping:", err)
	}

	bot, err = tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatal("Bot init:", err)
	}
	bot.Debug = false
	log.Printf("🤖 Go bot @%s\n", bot.Self.UserName)

	go startHTTPServer()
	startPolling()
}

func startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/notify", notifyHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})
	addr := ":3001"
	log.Println("📡 Go bot HTTP server on", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal("HTTP server:", err)
	}
}

func notifyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var n struct {
		UserID  int    `json:"user_id"`
		Title   string `json:"title"`
		Message string `json:"message"`
	}
	body, _ := io.ReadAll(r.Body)
	json.Unmarshal(body, &n)
	r.Body.Close()
	if n.UserID == 0 {
		w.WriteHeader(400)
		return
	}
	go func() {
		rows, err := db.Query("SELECT chat_id FROM telegram_links WHERE user_id=$1 AND notify_enabled=true", n.UserID)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var chatID int64
			rows.Scan(&chatID)
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("🔔 *%s*\n%s", n.Title, n.Message))
			msg.ParseMode = "Markdown"
			bot.Send(msg)
		}
	}()
	w.Write([]byte(`{"ok":true}`))
}

func startPolling() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		if update.Message != nil {
			go handleMessage(update.Message)
		}
		if update.CallbackQuery != nil {
			go handleCallback(update.CallbackQuery)
		}
	}
}

func backKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/start"),
		),
	)
}

func startKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚤 Лодки", "/boats"),
			tgbotapi.NewInlineKeyboardButtonData("🌤 Погода", "/weather"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 События", "/events"),
			tgbotapi.NewInlineKeyboardButtonData("🗺 Маршруты", "/route"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚤 Мои брони", "/rentals"),
			tgbotapi.NewInlineKeyboardButtonData("🎮 Игра", "/play"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👥 Мои команды", "/teams"),
			tgbotapi.NewInlineKeyboardButtonData("❓ Помощь", "/help"),
		),
	)
}

func send(chatID int64, text string, opts ...interface{}) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	for _, o := range opts {
		switch v := o.(type) {
		case tgbotapi.InlineKeyboardMarkup:
			msg.ReplyMarkup = v
		}
	}
	bot.Send(msg)
}

func edit(chatID int64, msgID int, text string, opts ...interface{}) {
	msg := tgbotapi.NewEditMessageText(chatID, msgID, text)
	msg.ParseMode = "Markdown"
	for _, o := range opts {
		switch v := o.(type) {
		case tgbotapi.InlineKeyboardMarkup:
			msg.ReplyMarkup = &v
		}
	}
	bot.Send(msg)
}

func handleMessage(m *tgbotapi.Message) {
	if !m.IsCommand() {
		return
	}
	switch m.Command() {
	case "start":
		send(m.Chat.ID, "🌊 *Тропы Каярана*\n━━━━━━━━━━━━━━━\nДобро пожаловать! Я — бот платформы водных активностей. Выбирай команду в меню ниже 👇", startKeyboard())
	case "help":
		send(m.Chat.ID, "🗺 *Помощь*\n━━━━━━━━━━━━━━━\n\n🔹 *Основное*\n/start — Главное меню\n/help — Эта справка\n\n🔹 *Активности*\n/boats — Статус лодок\n/weather — Погода на точках\n/events — Мероприятия\n/route — Маршруты\n\n🔹 *Личное*\n/profile — Мой профиль\n/rentals — Мои брони\n/teams — Мои команды\n/link email — Привязать аккаунт\n/notify — Уведомления\n\n🔹 *Развлечения*\n/play — SUP-Забег (игра)", backKeyboard())
	case "play":
		send(m.Chat.ID, "🎮 *SUP-Забег*\n━━━━━━━━━━━━━━━\n\nУправляй SUP-бордом, уклоняйся от камней и брёвен, собирай звёзды ⭐\n\n🏆 Набери максимум очков и поделись результатом с друзьями!",
			tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonURL("🎮 Запустить игру", gameURL),
				),
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/start"),
				),
			))
	case "boats":
		boatsHandler(m.Chat.ID)
	case "weather":
		weatherHandler(m.Chat.ID)
	case "events":
		eventsHandler(m.Chat.ID)
	case "teams":
		teamsHandler(m.Chat.ID, m.From.ID)
	case "profile":
		profileHandler(m.Chat.ID, m.From.ID)
	case "notify":
		notifyToggleHandler(m.Chat.ID, m.From.ID)
	case "rentals":
		rentalsHandler(m.Chat.ID, m.From.ID)
	case "route", "routes":
		routesHandler(m.Chat.ID)
	default:
		if strings.HasPrefix(m.Command(), "link") {
			linkHandler(m.Chat.ID, m.CommandArguments())
		}
	}
}

func handleCallback(cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	msgID := cb.Message.MessageID
	data := cb.Data
	bot.Send(tgbotapi.NewCallback(cb.ID, ""))

	switch data {
	case "/start":
		edit(chatID, msgID, "🌊 *Тропы Каярана*\n━━━━━━━━━━━━━━━\nДобро пожаловать! Я — бот платформы водных активностей. Выбирай команду в меню ниже 👇", startKeyboard())
	case "/help":
		edit(chatID, msgID, "🗺 *Помощь*\n━━━━━━━━━━━━━━━\n\n🔹 *Основное*\n/start — Главное меню\n/help — Эта справка\n\n🔹 *Активности*\n/boats — Статус лодок\n/weather — Погода на точках\n/events — Мероприятия\n/route — Маршруты\n\n🔹 *Личное*\n/profile — Мой профиль\n/rentals — Мои брони\n/teams — Мои команды\n/link email — Привязать аккаунт\n/notify — Уведомления\n\n🔹 *Развлечения*\n/play — SUP-Забег (игра)", backKeyboard())
	case "/play":
		edit(chatID, msgID, "🎮 *SUP-Забег*\n━━━━━━━━━━━━━━━\n\nУправляй SUP-бордом, уклоняйся от камней и брёвен, собирай звёзды ⭐\n\n🏆 Набери максимум очков и поделись результатом с друзьями!",
			tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonURL("🎮 Запустить игру", gameURL),
				),
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "/start"),
				),
			))
	case "/boats":
		boatsHandlerCb(chatID, msgID)
	case "/weather":
		weatherHandlerCb(chatID, msgID)
	case "/events":
		eventsHandlerCb(chatID, msgID)
	case "/teams":
		teamsHandlerCb(chatID, msgID, cb.From.ID)
	case "/profile":
		profileHandlerCb(chatID, msgID, cb.From.ID)
	case "/link_help":
		edit(chatID, msgID, "🔗 *Привязка аккаунта*\n━━━━━━━━━━━━━━━\n\nЧтобы привязать аккаунт из приложения:\n\n`/link твой@email.com твой_пароль`\n\nПосле привязки будут доступны:\n✅ /profile — профиль\n✅ /rentals — мои брони\n✅ /teams — мои команды\n✅ /notify — уведомления", backKeyboard())
	case "/rentals":
		rentalsHandlerCb(chatID, msgID, cb.From.ID)
	case "/route":
		routesHandlerCb(chatID, msgID)
	}
}

func boatsHandler(chatID int64) {
	rows, _ := queryCache("boats", "SELECT name, status FROM boats ORDER BY status, name")
	if len(rows) == 0 {
		send(chatID, "🚤 *Флот пуст*\nНет зарегистрированных лодок", backKeyboard())
		return
	}
	total := len(rows)
	avail, rented, repair := 0, 0, 0
	for _, r := range rows {
		switch fmt.Sprint(r["status"]) {
		case "available": avail++
		case "rented": rented++
		case "repair": repair++
		}
	}
	res := fmt.Sprintf("🚤 *Флот Троп Каярана*\n━━━━━━━━━━━━━━━\n\n📊 *Статистика:*\n┣ Всего: %d\n┣ ✅ Свободно: %d\n┣ 🔴 Занято: %d\n┗ 🔧 Ремонт: %d\n\n📋 *Список лодок:*\n", total, avail, rented, repair)
	for _, r := range rows {
		s := fmt.Sprint(r["status"])
		n := fmt.Sprint(r["name"])
		switch s {
		case "available": res += "┃ ✅ " + n + "\n"
		case "rented": res += "┃ 🔴 " + n + "\n"
		case "repair": res += "┃ 🔧 " + n + "\n"
		}
	}
	send(chatID, res, backKeyboard())
}

func boatsHandlerCb(chatID int64, msgID int) {
	rows, _ := queryCache("boats", "SELECT name, status FROM boats ORDER BY status, name")
	if len(rows) == 0 {
		edit(chatID, msgID, "🚤 *Флот пуст*\nНет зарегистрированных лодок", backKeyboard())
		return
	}
	total := len(rows)
	avail, rented, repair := 0, 0, 0
	for _, r := range rows {
		switch fmt.Sprint(r["status"]) {
		case "available": avail++
		case "rented": rented++
		case "repair": repair++
		}
	}
	res := fmt.Sprintf("🚤 *Флот Троп Каярана*\n━━━━━━━━━━━━━━━\n\n📊 *Статистика:*\n┣ Всего: %d\n┣ ✅ Свободно: %d\n┣ 🔴 Занято: %d\n┗ 🔧 Ремонт: %d\n\n📋 *Список лодок:*\n", total, avail, rented, repair)
	for _, r := range rows {
		s := fmt.Sprint(r["status"])
		n := fmt.Sprint(r["name"])
		switch s {
		case "available": res += "┃ ✅ " + n + "\n"
		case "rented": res += "┃ 🔴 " + n + "\n"
		case "repair": res += "┃ 🔧 " + n + "\n"
		}
	}
	edit(chatID, msgID, res, backKeyboard())
}

func weatherHandler(chatID int64) {
	rows, _ := queryCache("points", "SELECT name, lat, lng FROM points WHERE status=$1", "active")
	if len(rows) == 0 {
		send(chatID, "🌤 Нет активных точек", backKeyboard())
		return
	}
	results := make([]string, len(rows))
	var wg sync.WaitGroup
	for i, p := range rows {
		wg.Add(1)
		go func(idx int, point map[string]interface{}) {
			defer wg.Done()
			name := fmt.Sprint(point["name"])
			lat := fmt.Sprintf("%v", point["lat"])
			lng := fmt.Sprintf("%v", point["lng"])
			url := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s&current=temperature_2m,wind_speed_10m,weather_code", lat, lng)
			resp, err := http.Get(url)
			if err != nil {
				results[idx] = "📍 *" + name + "*: нет данных"
				return
			}
			defer resp.Body.Close()
			var d struct {
				Current struct {
					Temp  float64 `json:"temperature_2m"`
					Wind  float64 `json:"wind_speed_10m"`
					Code  int     `json:"weather_code"`
				} `json:"current"`
			}
			json.NewDecoder(resp.Body).Decode(&d)
			emoji := "☀️"
			if d.Current.Code > 0 {
				emoji = "⛅"
			}
			if d.Current.Code >= 3 {
				emoji = "☁️"
			}
			if d.Current.Code >= 50 {
				emoji = "🌧"
			}
			results[idx] = fmt.Sprintf("%s *%s*: %.0f°C, 💨 %.0f м/с", emoji, name, d.Current.Temp, d.Current.Wind)
		}(i, p)
	}
	wg.Wait()
	send(chatID, "🌤 *Погода на точках*\n━━━━━━━━━━━━━━━\n\n"+strings.Join(results, "\n"), backKeyboard())
}

func weatherHandlerCb(chatID int64, msgID int) {
	rows, _ := queryCache("points", "SELECT name, lat, lng FROM points WHERE status=$1", "active")
	if len(rows) == 0 {
		edit(chatID, msgID, "🌤 Нет активных точек", backKeyboard())
		return
	}
	results := make([]string, len(rows))
	var wg sync.WaitGroup
	for i, p := range rows {
		wg.Add(1)
		go func(idx int, point map[string]interface{}) {
			defer wg.Done()
			name := fmt.Sprint(point["name"])
			lat := fmt.Sprintf("%v", point["lat"])
			lng := fmt.Sprintf("%v", point["lng"])
			resp, err := http.Get(fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s&current=temperature_2m,wind_speed_10m,weather_code", lat, lng))
			if err != nil {
				results[idx] = "📍 *" + name + "*: нет данных"
				return
			}
			defer resp.Body.Close()
			var d struct {
				Current struct {
					Temp  float64 `json:"temperature_2m"`
					Wind  float64 `json:"wind_speed_10m"`
					Code  int     `json:"weather_code"`
				} `json:"current"`
			}
			json.NewDecoder(resp.Body).Decode(&d)
			emoji := "☀️"
			if d.Current.Code > 0 {
				emoji = "⛅"
			}
			if d.Current.Code >= 3 {
				emoji = "☁️"
			}
			if d.Current.Code >= 50 {
				emoji = "🌧"
			}
			results[idx] = fmt.Sprintf("%s *%s*: %.0f°C, 💨 %.0f м/с", emoji, name, d.Current.Temp, d.Current.Wind)
		}(i, p)
	}
	wg.Wait()
	edit(chatID, msgID, "🌤 *Погода на точках*\n━━━━━━━━━━━━━━━\n\n"+strings.Join(results, "\n"), backKeyboard())
}

func eventsHandler(chatID int64) {
	rows, _ := queryCache("events", "SELECT title, start_time FROM events WHERE start_time >= NOW() ORDER BY start_time LIMIT 5")
	if len(rows) == 0 {
		send(chatID, "📅 Нет мероприятий", backKeyboard())
		return
	}
	var b strings.Builder
	b.WriteString("📅 *Ближайшие мероприятия*\n━━━━━━━━━━━━━━━\n\n")
	for i, r := range rows {
		title := fmt.Sprint(r["title"])
		t := r["start_time"].(time.Time)
		b.WriteString(fmt.Sprintf("%d. *%s*\n   🕐 %s\n\n", i+1, title, t.Format("2 Jan 2006, 15:04")))
	}
	send(chatID, strings.TrimRight(b.String(), "\n"), backKeyboard())
}

func eventsHandlerCb(chatID int64, msgID int) {
	rows, _ := queryCache("events", "SELECT title, start_time FROM events WHERE start_time >= NOW() ORDER BY start_time LIMIT 5")
	if len(rows) == 0 {
		edit(chatID, msgID, "📅 Нет мероприятий", backKeyboard())
		return
	}
	var b strings.Builder
	b.WriteString("📅 *Ближайшие мероприятия*\n━━━━━━━━━━━━━━━\n\n")
	for i, r := range rows {
		title := fmt.Sprint(r["title"])
		t := r["start_time"].(time.Time)
		b.WriteString(fmt.Sprintf("%d. *%s*\n   🕐 %s\n\n", i+1, title, t.Format("2 Jan 2006, 15:04")))
	}
	edit(chatID, msgID, strings.TrimRight(b.String(), "\n"), backKeyboard())
}

func getUserID(tgID int64) (int, bool) {
	var uid int
	err := db.QueryRow("SELECT user_id FROM telegram_links WHERE chat_id=$1", strconv.FormatInt(tgID, 10)).Scan(&uid)
	if err != nil {
		return 0, false
	}
	return uid, true
}

func teamsHandler(chatID int64, tgID int64) {
	uid, ok := getUserID(tgID)
	if !ok {
		send(chatID, "🔗 *Аккаунт не привязан*\n━━━━━━━━━━━━━━━\n\nИспользуй команду:\n/link твой@email.com твой_пароль\n\nПосле этого будут доступны команды /profile и /teams.", backKeyboard())
		return
	}
	rows, err := db.Query("SELECT t.name FROM teams t JOIN team_members tm ON t.id=tm.team_id WHERE tm.user_id=$1", uid)
	if err != nil {
		return
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, "┃ 👥 "+n)
	}
	if len(names) == 0 {
		send(chatID, "👥 *Мои команды*\n━━━━━━━━━━━━━━━\n\nТы пока не состоишь ни в одной команде.\nВступи в команду в личном кабинете на сайте.")
		return
	}
	send(chatID, "👥 *Мои команды*\n━━━━━━━━━━━━━━━\n\nВсего команд: "+strconv.Itoa(len(names))+"\n\n"+strings.Join(names, "\n"), backKeyboard())
}

func teamsHandlerCb(chatID int64, msgID int, tgID int64) {
	uid, ok := getUserID(tgID)
	if !ok {
		edit(chatID, msgID, "🔗 *Аккаунт не привязан*\n━━━━━━━━━━━━━━━\n\nИспользуй команду:\n/link твой@email.com твой_пароль\n\nПосле этого будут доступны команды /profile и /teams.", backKeyboard())
		return
	}
	rows, err := db.Query("SELECT t.name FROM teams t JOIN team_members tm ON t.id=tm.team_id WHERE tm.user_id=$1", uid)
	if err != nil {
		return
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, "┃ 👥 "+n)
	}
	if len(names) == 0 {
		edit(chatID, msgID, "👥 *Мои команды*\n━━━━━━━━━━━━━━━\n\nТы пока не состоишь ни в одной команде.\nВступи в команду в личном кабинете на сайте.")
		return
	}
	edit(chatID, msgID, "👥 *Мои команды*\n━━━━━━━━━━━━━━━\n\nВсего команд: "+strconv.Itoa(len(names))+"\n\n"+strings.Join(names, "\n"), backKeyboard())
}

func profileHandler(chatID int64, tgID int64) {
	uid, ok := getUserID(tgID)
	if !ok {
		send(chatID, "🔗 *Аккаунт не привязан*\n━━━━━━━━━━━━━━━\n\nИспользуй команду:\n/link твой@email.com твой_пароль", backKeyboard())
		return
	}
	var u struct{ name, email, role string }
	err := db.QueryRow("SELECT name, email, role FROM users WHERE id=$1", uid).Scan(&u.name, &u.email, &u.role)
	if err != nil {
		send(chatID, "❌ *Ошибка*\nПользователь не найден в системе")
		return
	}
	roleEmoji := "👤"
	switch u.role {
	case "admin": roleEmoji = "🛡️"
	case "instructor": roleEmoji = "🧭"
	case "manager": roleEmoji = "📋"
	}
	var cnt int
	db.QueryRow("SELECT COUNT(*)::int FROM rentals WHERE user_id=$1", uid).Scan(&cnt)
	send(chatID, fmt.Sprintf("👤 *Профиль пользователя*\n━━━━━━━━━━━━━━━\n\n┃ 👤 Имя: *%s*\n┃ 📧 Email: `%s`\n┃ %s Роль: *%s*\n┃ 🚤 Аренд: *%d*\n━━━━━━━━━━━━━━━", u.name, u.email, roleEmoji, u.role, cnt), backKeyboard())
}

func profileHandlerCb(chatID int64, msgID int, tgID int64) {
	uid, ok := getUserID(tgID)
	if !ok {
		edit(chatID, msgID, "🔗 *Аккаунт не привязан*\n━━━━━━━━━━━━━━━\n\nИспользуй команду:\n/link твой@email.com твой_пароль", backKeyboard())
		return
	}
	var u struct{ name, email, role string }
	err := db.QueryRow("SELECT name, email, role FROM users WHERE id=$1", uid).Scan(&u.name, &u.email, &u.role)
	if err != nil {
		edit(chatID, msgID, "❌ *Ошибка*\nПользователь не найден в системе")
		return
	}
	roleEmoji := "👤"
	switch u.role {
	case "admin": roleEmoji = "🛡️"
	case "instructor": roleEmoji = "🧭"
	case "manager": roleEmoji = "📋"
	}
	var cnt int
	db.QueryRow("SELECT COUNT(*)::int FROM rentals WHERE user_id=$1", uid).Scan(&cnt)
	edit(chatID, msgID, fmt.Sprintf("👤 *Профиль пользователя*\n━━━━━━━━━━━━━━━\n\n┃ 👤 Имя: *%s*\n┃ 📧 Email: `%s`\n┃ %s Роль: *%s*\n┃ 🚤 Аренд: *%d*\n━━━━━━━━━━━━━━━", u.name, u.email, roleEmoji, u.role, cnt), backKeyboard())
}

func rentalsHandler(chatID int64, tgID int64) {
	uid, ok := getUserID(tgID)
	if !ok {
		send(chatID, "🔗 *Аккаунт не привязан*\n━━━━━━━━━━━━━━━\n\nИспользуй команду:\n/link твой@email.com твой_пароль", backKeyboard())
		return
	}
	rows, err := db.Query(`SELECT r.status, r.start_time, COALESCE(r.total_amount,0)::int, b.name AS boat, e.title AS event
		FROM rentals r JOIN boats b ON r.boat_id=b.id JOIN events e ON r.event_id=e.id
		WHERE r.user_id=$1 ORDER BY r.start_time DESC LIMIT 10`, uid)
	if err != nil {
		send(chatID, "❌ *Ошибка*\nНе удалось загрузить брони")
		return
	}
	defer rows.Close()
	type rental struct{ status, boat, event string; start time.Time; amount int }
	var list []rental
	for rows.Next() {
		var r rental
		rows.Scan(&r.status, &r.start, &r.amount, &r.boat, &r.event)
		list = append(list, r)
	}
	if len(list) == 0 {
		send(chatID, "🚤 *Мои брони*\n━━━━━━━━━━━━━━━\n\nУ тебя пока нет бронирований.\nНачни с /events — выбери мероприятие!", backKeyboard())
		return
	}
	emoji := map[string]string{"booked": "📋", "active": "🟢", "completed": "✅", "cancelled": "❌"}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🚤 *Мои брони* (%d)\n━━━━━━━━━━━━━━━\n\n", len(list)))
	for i, r := range list {
		e := emoji[r.status]
		if e == "" { e = "📋" }
		b.WriteString(fmt.Sprintf("%d. %s *%s*\n   ┣ 🚤 %s\n   ┣ 📅 %s\n   ┗ 💰 %d₽\n\n", i+1, e, r.event, r.boat, r.start.Format("2 Jan 15:04"), r.amount))
	}
	send(chatID, strings.TrimRight(b.String(), "\n"), backKeyboard())
}

func rentalsHandlerCb(chatID int64, msgID int, tgID int64) {
	uid, ok := getUserID(tgID)
	if !ok {
		edit(chatID, msgID, "🔗 *Аккаунт не привязан*\n━━━━━━━━━━━━━━━\n\nИспользуй команду:\n/link твой@email.com твой_пароль", backKeyboard())
		return
	}
	rows, err := db.Query(`SELECT r.status, r.start_time, COALESCE(r.total_amount,0)::int, b.name AS boat, e.title AS event
		FROM rentals r JOIN boats b ON r.boat_id=b.id JOIN events e ON r.event_id=e.id
		WHERE r.user_id=$1 ORDER BY r.start_time DESC LIMIT 10`, uid)
	if err != nil {
		edit(chatID, msgID, "❌ *Ошибка*\nНе удалось загрузить брони")
		return
	}
	defer rows.Close()
	type rental struct{ status, boat, event string; start time.Time; amount int }
	var list []rental
	for rows.Next() {
		var r rental
		rows.Scan(&r.status, &r.start, &r.amount, &r.boat, &r.event)
		list = append(list, r)
	}
	if len(list) == 0 {
		edit(chatID, msgID, "🚤 *Мои брони*\n━━━━━━━━━━━━━━━\n\nУ тебя пока нет бронирований.\nНачни с /events — выбери мероприятие!", backKeyboard())
		return
	}
	emoji := map[string]string{"booked": "📋", "active": "🟢", "completed": "✅", "cancelled": "❌"}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🚤 *Мои брони* (%d)\n━━━━━━━━━━━━━━━\n\n", len(list)))
	for i, r := range list {
		e := emoji[r.status]
		if e == "" { e = "📋" }
		b.WriteString(fmt.Sprintf("%d. %s *%s*\n   ┣ 🚤 %s\n   ┣ 📅 %s\n   ┗ 💰 %d₽\n\n", i+1, e, r.event, r.boat, r.start.Format("2 Jan 15:04"), r.amount))
	}
	edit(chatID, msgID, strings.TrimRight(b.String(), "\n"), backKeyboard())
}

func routesHandler(chatID int64) {
	rows, _ := queryCache("routes", "SELECT title, difficulty, distance_km, COALESCE(description,'') as descr, is_inclusive FROM routes WHERE status='active' ORDER BY difficulty, title")
	if len(rows) == 0 {
		send(chatID, "🗺 *Маршруты*\n━━━━━━━━━━━━━━━\n\nНет активных маршрутов", backKeyboard())
		return
	}
	difficultyEmoji := map[string]string{"easy": "🟢", "medium": "🟡", "hard": "🟠", "extreme": "🔴"}
	difficultyLabel := map[string]string{"easy": "Лёгкий", "medium": "Средний", "hard": "Сложный", "extreme": "Экстремальный"}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🗺 *Маршруты* (%d)\n━━━━━━━━━━━━━━━\n\n", len(rows)))
	for i, r := range rows {
		title := fmt.Sprint(r["title"])
		diff := fmt.Sprint(r["difficulty"])
		dist := fmt.Sprintf("%v", r["distance_km"])
		desc := fmt.Sprint(r["descr"])
		incl := false
		if v, ok := r["is_inclusive"].(bool); ok { incl = v }
		e := difficultyEmoji[diff]
		dl := difficultyLabel[diff]
		if e == "" { e = "⬜"; dl = diff }
		inclTag := ""
		if incl { inclTag = " ♿" }
		b.WriteString(fmt.Sprintf("%d. %s *%s*%s\n   ┣ 🏔 %s\n   ┣ 📏 %s км\n", i+1, e, title, inclTag, dl, dist))
		if len(desc) > 0 {
			b.WriteString(fmt.Sprintf("   ┗ %s\n", desc))
		}
		b.WriteString("\n")
	}
	send(chatID, strings.TrimRight(b.String(), "\n"), backKeyboard())
}

func routesHandlerCb(chatID int64, msgID int) {
	rows, _ := queryCache("routes", "SELECT title, difficulty, distance_km, COALESCE(description,'') as descr, is_inclusive FROM routes WHERE status='active' ORDER BY difficulty, title")
	if len(rows) == 0 {
		edit(chatID, msgID, "🗺 *Маршруты*\n━━━━━━━━━━━━━━━\n\nНет активных маршрутов", backKeyboard())
		return
	}
	difficultyEmoji := map[string]string{"easy": "🟢", "medium": "🟡", "hard": "🟠", "extreme": "🔴"}
	difficultyLabel := map[string]string{"easy": "Лёгкий", "medium": "Средний", "hard": "Сложный", "extreme": "Экстремальный"}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🗺 *Маршруты* (%d)\n━━━━━━━━━━━━━━━\n\n", len(rows)))
	for i, r := range rows {
		title := fmt.Sprint(r["title"])
		diff := fmt.Sprint(r["difficulty"])
		dist := fmt.Sprintf("%v", r["distance_km"])
		desc := fmt.Sprint(r["descr"])
		incl := false
		if v, ok := r["is_inclusive"].(bool); ok { incl = v }
		e := difficultyEmoji[diff]
		dl := difficultyLabel[diff]
		if e == "" { e = "⬜"; dl = diff }
		inclTag := ""
		if incl { inclTag = " ♿" }
		b.WriteString(fmt.Sprintf("%d. %s *%s*%s\n   ┣ 🏔 %s\n   ┣ 📏 %s км\n", i+1, e, title, inclTag, dl, dist))
		if len(desc) > 0 {
			b.WriteString(fmt.Sprintf("   ┗ %s\n", desc))
		}
		b.WriteString("\n")
	}
	edit(chatID, msgID, strings.TrimRight(b.String(), "\n"), backKeyboard())
}

func linkHandler(chatID int64, args string) {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		send(chatID, "❌ *Ошибка*\n━━━━━━━━━━━━━━━\n\nФормат: `/link email пароль`\n\nПример: `/link ivan@mail.ru password123`", backKeyboard())
		return
	}
	email := parts[0]
	var uid int
	err := db.QueryRow("SELECT id FROM users WHERE email=$1", email).Scan(&uid)
	if err != nil {
		send(chatID, "❌ *Пользователь не найден*\n\nПользователь с email `"+email+"` не зарегистрирован в системе.", backKeyboard())
		return
	}
	db.Exec("INSERT INTO telegram_links (chat_id, user_id) VALUES ($1,$2) ON CONFLICT (chat_id) DO UPDATE SET user_id=$2",
		strconv.FormatInt(chatID, 10), uid)
	send(chatID, "✅ *Аккаунт привязан!*\n━━━━━━━━━━━━━━━\n\nТеперь тебе доступны команды:\n┣ /profile — профиль\n┣ /teams — мои команды\n┗ /notify — уведомления", backKeyboard())
}

func notifyToggleHandler(chatID int64, tgID int64) {
	uid, ok := getUserID(tgID)
	if !ok {
		send(chatID, "🔗 *Аккаунт не привязан*\n━━━━━━━━━━━━━━━\n\nИспользуй команду:\n/link твой@email.com твой_пароль", tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🔗 Привязать", "/link_help"),
			),
		))
		return
	}
	var enabled bool
	db.QueryRow("SELECT notify_enabled FROM telegram_links WHERE user_id=$1", uid).Scan(&enabled)
	enabled = !enabled
	db.Exec("UPDATE telegram_links SET notify_enabled=$1 WHERE user_id=$2", enabled, uid)
	if enabled {
		send(chatID, "🔔 *Уведомления*\n━━━━━━━━━━━━━━━\n\nУведомления ✅ *включены*\nТеперь ты будешь получать оповещения о бронированиях и мероприятиях.", backKeyboard())
	} else {
		send(chatID, "🔔 *Уведомления*\n━━━━━━━━━━━━━━━\n\nУведомления ❌ *выключены*", backKeyboard())
	}
}