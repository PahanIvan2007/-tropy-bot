package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/SevereCloud/vksdk/v3/api"
	"github.com/SevereCloud/vksdk/v3/events"
	"github.com/SevereCloud/vksdk/v3/longpoll-bot"
	"github.com/SevereCloud/vksdk/v3/object"
)

var vk *api.VK
var vkToken string
var vkGroupID int

func startVKBot() {
	vkToken = os.Getenv("VK_BOT_TOKEN")
	if vkToken == "" {
		log.Println("⚠️ VK_BOT_TOKEN not set, VK bot disabled")
		return
	}

	vk = api.NewVK(vkToken)
	group, err := vk.GroupsGetByID(nil)
	if err != nil {
		log.Println("VK group get error:", err)
		return
	}
	if len(group.Groups) == 0 {
		log.Println("VK: no group found")
		return
	}
	vkGroupID = group.Groups[0].ID
	log.Printf("🤖 VK bot @%s (id=%d)\n", group.Groups[0].ScreenName, vkGroupID)

	lp, err := longpoll.NewLongPoll(vk, vkGroupID)
	if err != nil {
		log.Println("VK LongPoll error:", err)
		return
	}

	lp.MessageNew(func(_ context.Context, obj events.MessageNewObject) {
		go handleVKMessage(obj.Message)
	})

	log.Println("📡 VK LongPoll started")
	if err := lp.Run(); err != nil {
		log.Println("VK LongPoll stopped:", err)
	}
}

func vkSend(peerID int, text string, keyboard ...string) {
	p := api.Params{
		"peer_id":   peerID,
		"message":   text,
		"random_id": 0,
	}
	if len(keyboard) > 0 && keyboard[0] != "" {
		p["keyboard"] = keyboard[0]
	}
	vk.MessagesSend(p)
}

func vkKeyboard() string {
	return `{"one_time":false,"buttons":[
		[{"action":{"type":"text","label":"🚤 Лодки","payload":"\"boats\""},"color":"primary"},
		 {"action":{"type":"text","label":"🌤 Погода","payload":"\"weather\""},"color":"primary"}],
		[{"action":{"type":"text","label":"📅 События","payload":"\"events\""},"color":"primary"},
		 {"action":{"type":"text","label":"🗺 Маршруты","payload":"\"routes\""},"color":"primary"}],
		[{"action":{"type":"text","label":"🎮 SUP-Забег","payload":"\"play\""},"color":"positive"},
		 {"action":{"type":"text","label":"❓ Помощь","payload":"\"help\""},"color":"secondary"}]
	]}`
}

func vkBackKeyboard() string {
	return `{"one_time":false,"buttons":[
		[{"action":{"type":"text","label":"🔙 Назад","payload":"\"start\""},"color":"secondary"}]
	]}`
}

func handleVKMessage(msg object.MessagesMessage) {
	if msg.PeerID == 0 || msg.Text == "" {
		return
	}
	peerID := msg.PeerID
	text := strings.TrimSpace(msg.Text)

	// Убираем эмодзи-префиксы для сравнения
	clean := text
	for _, p := range []string{"🚤 ", "🌤 ", "📅 ", "🗺 ", "🎮 ", "❓ ", "🔙 "} {
		clean = strings.TrimPrefix(clean, p)
	}
	clean = strings.TrimSpace(clean)

	switch {
	case text == "/start" || text == "Начать" || text == "start" || clean == "Назад":
		vkSend(peerID, "🌊 Тропы Каярана\nДобро пожаловать! Выбирай команду в меню ниже 👇", vkKeyboard())
	case text == "/help" || text == "help" || text == "Помощь" || clean == "Помощь":
		vkSend(peerID, "🗺 Помощь\n\n🔹 Основное:\n/start — Главное меню\n/help — Эта справка\n\n🔹 Активности:\n/boats — Статус лодок\n/weather — Погода на точках\n/events — Мероприятия\n/routes — Маршруты\n\n🔹 Аккаунт:\n/register Имя Фамилия email пароль — Регистрация\n/link email — Привязать существующий аккаунт\n\n🔹 Развлечения:\n/play — SUP-Забег (игра)", vkBackKeyboard())
	case text == "/play" || text == "play" || text == "Игра" || strings.Contains(text, "SUP-Забег") || clean == "SUP-Забег":
		vkSend(peerID, "🎮 SUP-Забег\n\nУправляй SUP-бордом, уклоняйся от камней и брёвен, собирай звёзды ⭐\n\nОткрой игру: https://tropy-kayrana-bot.onrender.com/game/")
	case text == "/boats" || text == "boats" || clean == "Лодки":
		vkBoatHandler(peerID)
	case text == "/weather" || text == "weather" || clean == "Погода":
		vkWeatherHandler(peerID)
	case text == "/events" || text == "events" || clean == "События":
		vkEventsHandler(peerID)
	case text == "/routes" || text == "routes" || clean == "Маршруты":
		vkRoutesHandler(peerID)
	default:
		if strings.HasPrefix(text, "/link") {
			vkLinkHandler(peerID, strings.TrimPrefix(text, "/link"))
		} else if strings.HasPrefix(text, "/register") {
			vkRegisterHandler(peerID, strings.TrimPrefix(text, "/register"))
		} else {
			vkSend(peerID, "❌ Неизвестная команда. Напиши /help для списка команд.")
		}
	}
}

func vkBoatHandler(peerID int) {
	rows, _ := queryCache("boats", "SELECT name, status FROM boats ORDER BY status, name")
	if len(rows) == 0 {
		vkSend(peerID, "🚤 Флот пуст. Нет зарегистрированных лодок.")
		return
	}
	total := len(rows)
	avail, rented, repair := 0, 0, 0
	for _, r := range rows {
		switch fmt.Sprint(r["status"]) {
		case "available":
			avail++
		case "rented":
			rented++
		case "maintenance":
			repair++
		}
	}
	res := fmt.Sprintf("🚤 Флот Троп Каярана\n\n📊 Статистика:\n┣ Всего: %d\n┣ ✅ Свободно: %d\n┣ 🔴 Занято: %d\n┗ 🔧 Ремонт: %d\n\n📋 Список:\n", total, avail, rented, repair)
	for _, r := range rows {
		s := fmt.Sprint(r["status"])
		n := fmt.Sprint(r["name"])
		switch s {
		case "available":
			res += "┃ ✅ " + n + "\n"
		case "rented":
			res += "┃ 🔴 " + n + "\n"
		case "maintenance":
			res += "┃ 🔧 " + n + "\n"
		}
	}
	vkSend(peerID, res)
}

func vkWeatherHandler(peerID int) {
	rows, _ := queryCache("points", "SELECT name, lat, lng FROM points WHERE status=$1", "active")
	if len(rows) == 0 {
		vkSend(peerID, "🌤 Нет активных точек")
		return
	}
	results := make([]string, len(rows))
	for i, p := range rows {
		name := fmt.Sprint(p["name"])
		lat := fmt.Sprintf("%v", p["lat"])
		lng := fmt.Sprintf("%v", p["lng"])
		url := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s&current=temperature_2m,wind_speed_10m,weather_code", lat, lng)
		resp, err := httpGet(url)
		if err != nil {
			results[i] = "📍 " + name + ": нет данных"
			continue
		}
		var d struct {
			Current struct {
				Temp float64 `json:"temperature_2m"`
				Wind float64 `json:"wind_speed_10m"`
				Code int     `json:"weather_code"`
			} `json:"current"`
		}
		json.Unmarshal(resp, &d)
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
		results[i] = fmt.Sprintf("%s %s: %.0f°C, 💨 %.0f м/с", emoji, name, d.Current.Temp, d.Current.Wind)
	}
		vkSend(peerID, "🌤 Погода на точках\n\n"+strings.Join(results, "\n"))
}

func vkEventsHandler(peerID int) {
	rows, _ := queryCache("events", "SELECT title, start_time FROM events WHERE start_time >= NOW() ORDER BY start_time LIMIT 5")
	if len(rows) == 0 {
		vkSend(peerID, "📅 Нет мероприятий")
		return
	}
	var b strings.Builder
	b.WriteString("📅 Ближайшие мероприятия\n\n")
	for i, r := range rows {
		title := fmt.Sprint(r["title"])
		t := r["start_time"].(time.Time)
		b.WriteString(fmt.Sprintf("%d. %s\n   🕐 %s\n\n", i+1, title, t.Format("2 Jan 2006, 15:04")))
	}
	vkSend(peerID, strings.TrimRight(b.String(), "\n"))
}

func vkRoutesHandler(peerID int) {
	rows, _ := queryCache("routes", "SELECT title, difficulty, distance_km, is_inclusive FROM routes WHERE status='active' ORDER BY difficulty, title")
	if len(rows) == 0 {
		vkSend(peerID, "🗺 Маршруты\n\nНет активных маршрутов")
		return
	}
	difficultyEmoji := map[string]string{"easy": "🟢", "medium": "🟡", "hard": "🟠", "extreme": "🔴"}
	difficultyLabel := map[string]string{"easy": "Лёгкий", "medium": "Средний", "hard": "Сложный", "extreme": "Экстремальный"}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🗺 *Маршруты* (%d)\n\n", len(rows)))
	for i, r := range rows {
		title := fmt.Sprint(r["title"])
		diff := fmt.Sprint(r["difficulty"])
		dist := fmt.Sprintf("%v", r["distance_km"])
		incl := false
		if v, ok := r["is_inclusive"].(bool); ok {
			incl = v
		}
		e := difficultyEmoji[diff]
		dl := difficultyLabel[diff]
		if e == "" {
			e = "⬜"
			dl = diff
		}
		inclTag := ""
		if incl {
			inclTag = " ♿"
		}
		b.WriteString(fmt.Sprintf("%d. %s %s%s\n   🏔 %s\n   📏 %s км\n\n", i+1, e, title, inclTag, dl, dist))
	}
	vkSend(peerID, strings.TrimRight(b.String(), "\n"))
}

func vkLinkHandler(peerID int, args string) {
	parts := strings.Fields(args)
	if len(parts) < 2 {
		vkSend(peerID, "❌ Формат: /link email пароль\n\nПример: /link ivan@mail.ru password123")
		return
	}
	email := parts[0]
	var uid int
	err := db.QueryRow("SELECT id FROM users WHERE email=$1", email).Scan(&uid)
	if err != nil {
		vkSend(peerID, "❌ Пользователь с email "+email+" не найден в системе.")
		return
	}
	db.Exec("INSERT INTO vk_links (peer_id, user_id) VALUES ($1,$2) ON CONFLICT (peer_id) DO UPDATE SET user_id=$2",
		strconv.Itoa(peerID), uid)
	vkSend(peerID, "✅ Аккаунт привязан! Теперь тебе доступны профиль и брони.")
}

const apiBase = "https://tropy-kayrana-api.onrender.com"

func vkRegisterHandler(peerID int, args string) {
	args = strings.TrimSpace(args)
	parts := strings.SplitN(args, " ", 4)
	if len(parts) < 4 {
		vkSend(peerID, "❌ Формат: /register Имя Фамилия email пароль\n\nПример: /register Иван Иванов ivan@example.com password123")
		return
	}
	firstName := strings.TrimSpace(parts[0])
	lastName := strings.TrimSpace(parts[1])
	email := strings.TrimSpace(parts[2])
	password := strings.TrimSpace(parts[3])

	if len(password) < 6 {
		vkSend(peerID, "❌ Пароль должен быть минимум 6 символов.")
		return
	}

	body := map[string]string{
		"first_name": firstName,
		"last_name":  lastName,
		"email":      email,
		"password":   password,
	}
	resp, err := httpPostJSON(apiBase+"/api/auth/register", body)
	if err != nil {
		log.Println("Register API error:", err)
		vkSend(peerID, "❌ Ошибка сервера. Попробуй позже.")
		return
	}

	var result struct {
		User  map[string]interface{} `json:"user"`
		Token string                 `json:"token"`
	}
	if err := json.Unmarshal(resp, &result); err != nil || result.User == nil {
		var errResp struct {
			Error string `json:"error"`
		}
		json.Unmarshal(resp, &errResp)
		msg := errResp.Error
		if msg == "" {
			msg = "Регистрация не удалась. Возможно, email уже занят."
		}
		vkSend(peerID, "❌ "+msg)
		return
	}

	uid := int(result.User["id"].(float64))
	db.Exec("INSERT INTO vk_links (peer_id, user_id) VALUES ($1,$2) ON CONFLICT (peer_id) DO UPDATE SET user_id=$2",
		strconv.Itoa(peerID), uid)

	vkSend(peerID, "✅ Регистрация прошла успешно, "+firstName+"!\n\nТвой аккаунт привязан к ВК. Используй /help для списка команд.")
}

func httpGet(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func httpPostJSON(url string, data interface{}) ([]byte, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
