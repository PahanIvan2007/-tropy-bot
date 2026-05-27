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
	go startVKBot()
	startPolling()
}

func startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/notify", notifyHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/game/", gameHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/game/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})
	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}
	addr := ":" + port
	log.Println("📡 Go bot HTTP server on", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal("HTTP server:", err)
	}
}

func gameHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(gameHTML)
}

var gameHTML = []byte(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">
<title>SUP-Забег</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#0a1628;overflow:hidden;touch-action:none;font-family:Arial,sans-serif}
canvas{display:block;margin:0 auto;background:linear-gradient(180deg,#0a1628 0%,#1a3a5c 40%,#2a6a9e 70%,#3a8abe 100%)}
#ui{position:absolute;top:10px;left:0;right:0;display:flex;justify-content:space-between;padding:0 20px;pointer-events:none;z-index:10}
#score{color:#ffd700;font-size:24px;font-weight:bold;text-shadow:0 2px 4px rgba(0,0,0,.5)}
#best{color:#fff;font-size:18px;opacity:.7}
#gameOver{position:absolute;top:0;left:0;right:0;bottom:0;display:none;flex-direction:column;align-items:center;justify-content:center;background:rgba(0,0,0,.7);z-index:20}
#gameOver h2{color:#ffd700;font-size:32px;margin-bottom:10px}
#gameOver p{color:#fff;font-size:18px;margin-bottom:20px}
#gameOver button{padding:12px 40px;font-size:20px;background:#ffd700;color:#0a1628;border:none;border-radius:25px;cursor:pointer;font-weight:bold}
#startScreen{position:absolute;top:0;left:0;right:0;bottom:0;display:flex;flex-direction:column;align-items:center;justify-content:center;background:rgba(0,0,0,.6);z-index:15}
#startScreen h1{color:#ffd700;font-size:36px;margin-bottom:5px}
#startScreen p{color:#aaa;font-size:14px;margin-bottom:25px}
#startScreen button{padding:12px 40px;font-size:20px;background:#ffd700;color:#0a1628;border:none;border-radius:25px;cursor:pointer;font-weight:bold}
.info{color:#888;font-size:12px;margin-top:10px}
</style>
</head>
<body>
<div id="ui"><div id="score">⭐ 0</div><div id="best">🏆 0</div></div>
<div id="startScreen"><h1>🏄 SUP-Забег</h1><p>Управляй тач/мышь • уклоняйся от препятствий</p><button id="startBtn">Старт!</button><div class="info">⬅️ ➡️ или тач влево/вправо</div></div>
<div id="gameOver"><h2>💥 Конец!</h2><p id="finalScore">Очки: 0</p><button id="restartBtn">Ещё раз</button></div>
<canvas id="game"></canvas>
<script>
const canvas=document.getElementById('game'),ctx=canvas.getContext('2d');
let W=400,H=700,score=0,bestScore=parseInt(localStorage.getItem('supBest')||'0'),running=false;
function resize(){const m=Math.min(window.innerWidth,400);W=m;H=Math.min(window.innerHeight,700);canvas.width=W;canvas.height=H;dpr=W/400}
let dpr=1;resize();window.addEventListener('resize',resize);
const player={x:200,y:620,w:40,h:50,speed:0,score:0};
let obstacles=[],stars=[],frame=0,targetX=200;
document.getElementById('best').innerHTML='🏆 '+bestScore;
function resetGame(){player.x=200;player.y=620;player.speed=0;player.score=0;score=0;obstacles=[];stars=[];frame=0;targetX=200;document.getElementById('score').innerHTML='⭐ 0'}
function spawnObstacle(){const types=['rock','log','buoy'];const t=types[Math.floor(Math.random()*types.length)];const w=t==='rock'?40+Math.random()*30:30+Math.random()*30;const h=t==='log'?20+Math.random()*15:30+Math.random()*20;obstacles.push({x:Math.random()*(W-w),y:-h,w,h,type:t,speed:2+Math.random()*2})}
function spawnStar(){stars.push({x:10+Math.random()*(W-20),y:-10,w:20,h:20,type:'star',speed:2+Math.random()*1.5})}
function update(){frame++;if(frame%40===0)spawnObstacle();if(frame%60===0)spawnStar();player.speed+=0.05;if(player.speed>5)player.speed=5;const dx=targetX-player.x;player.x+=dx*0.15;if(player.x<0)player.x=0;if(player.x>W-player.w)player.x=W-player.w;for(let i=obstacles.length-1;i>=0;i--){const o=obstacles[i];o.y+=o.speed;if(o.y>H+50){obstacles.splice(i,1);continue}const px=player.x,py=player.y,pw=player.w,ph=player.h;if(px+pw>o.x&&px<o.x+o.w&&py+ph>o.y&&py<o.y+o.h){endGame();return}}for(let i=stars.length-1;i>=0;i--){const s=stars[i];s.y+=s.speed;if(s.y>H+50){stars.splice(i,1);continue}const px=player.x,py=player.y,pw=player.w,ph=player.h;if(px+pw>s.x&&px<s.x+s.w&&py+ph>s.y&&py<s.y+s.h){stars.splice(i,1);score++;player.score=score;document.getElementById('score').innerHTML='⭐ '+score;if(score>bestScore){bestScore=score;localStorage.setItem('supBest',bestScore.toString());document.getElementById('best').innerHTML='🏆 '+bestScore}}}}
function draw(){ctx.clearRect(0,0,W,H);const grad=ctx.createLinearGradient(0,0,0,H);grad.addColorStop(0,'#0a1628');grad.addColorStop(0.3,'#1a3a5c');grad.addColorStop(0.6,'#2a6a9e');grad.addColorStop(1,'#3a8abe');ctx.fillStyle=grad;ctx.fillRect(0,0,W,H);for(let i=0;i<8;i++){const y=(frame*0.3+i*60)%(H+40)-40;ctx.fillStyle='rgba(255,255,255,0.03)';ctx.fillRect(0,y,W,2)}for(const o of obstacles){ctx.save();if(o.type==='rock'){ctx.fillStyle='#5a4a3a';ctx.beginPath();const cx=o.x+o.w/2,cy=o.y+o.h/2;ctx.ellipse(cx,cy,o.w/2,o.h/2,0,0,Math.PI*2);ctx.fill();ctx.fillStyle='#3a2a1a';ctx.beginPath();ctx.ellipse(cx-3,cy-3,o.w*0.3,o.h*0.3,0,0,Math.PI*2);ctx.fill()}else if(o.type==='log'){ctx.fillStyle='#6b4226';const r=o.h/2;ctx.beginPath();ctx.roundRect(o.x,o.y,o.w,o.h,r,r);ctx.fill();ctx.fillStyle='#4a2a16';for(let i=0;i<3;i++){const lx=o.x+5+i*(o.w-10)/2;ctx.beginPath();ctx.arc(lx,o.y+o.h/2,2,0,Math.PI*2);ctx.fill()}}else{ctx.fillStyle='#e04040';ctx.beginPath();ctx.arc(o.x+o.w/2,o.y+o.h/2,o.w/2,0,Math.PI*2);ctx.fill();ctx.fillStyle='#ffffff';ctx.beginPath();ctx.arc(o.x+o.w/2-3,o.y+o.h/2-3,3,0,Math.PI*2);ctx.fill()}ctx.restore()}for(const s of stars){ctx.save();ctx.fillStyle='#ffd700';const cx=s.x+s.w/2,cy=s.y+s.h/2;const r=s.w/2;ctx.beginPath();for(let i=0;i<5;i++){const angle=-Math.PI/2+i*Math.PI*2/5;const px=cx+Math.cos(angle)*r;const py=cy+Math.sin(angle)*r;i===0?ctx.moveTo(px,py):ctx.lineTo(px,py);const angle2=angle+Math.PI/5;const px2=cx+Math.cos(angle2)*r*0.4;const py2=cy+Math.sin(angle2)*r*0.4;ctx.lineTo(px2,py2)}ctx.closePath();ctx.fill();ctx.fillStyle='rgba(255,215,0,0.3)';ctx.beginPath();ctx.arc(cx,cy,r+5,0,Math.PI*2);ctx.fill();ctx.restore()}ctx.save();ctx.fillStyle='#e07040';ctx.beginPath();ctx.ellipse(player.x+player.w/2,player.y+player.h-5,player.w/2,15,0,0,Math.PI*2);ctx.fill();ctx.fillStyle='#d06030';ctx.fillRect(player.x+5,player.y+player.h-8,player.w-10,6);ctx.fillStyle='#4a8';(player.frame%10<5)?ctx.fillStyle='#ff8c42':ctx.fillStyle='#3a6';ctx.beginPath();ctx.ellipse(player.x+player.w/2,player.y-5,player.w*0.35,player.h*0.2,0,0,Math.PI*2);ctx.fill();ctx.fillStyle='#fff';ctx.beginPath();ctx.arc(player.x+player.w/2-5,player.y-8,2,0,Math.PI*2);ctx.fill();ctx.beginPath();ctx.arc(player.x+player.w/2+5,player.y-8,2,0,Math.PI*2);ctx.fill();ctx.restore();if(running)requestAnimationFrame(gameLoop)}
function gameLoop(){update();draw()}
function endGame(){running=false;document.getElementById('finalScore').innerHTML='Очки: '+score;document.getElementById('gameOver').style.display='flex'}
canvas.addEventListener('mousemove',e=>{const rect=canvas.getBoundingClientRect();targetX=(e.clientX-rect.left)*dpr-player.w/2});canvas.addEventListener('touchstart',e=>{e.preventDefault();const t=e.touches[0];const rect=canvas.getBoundingClientRect();targetX=(t.clientX-rect.left)*dpr-player.w/2});canvas.addEventListener('touchmove',e=>{e.preventDefault();const t=e.touches[0];const rect=canvas.getBoundingClientRect();targetX=(t.clientX-rect.left)*dpr-player.w/2});
document.getElementById('startBtn').addEventListener('click',()=>{document.getElementById('startScreen').style.display='none';resetGame();running=true;gameLoop()});
document.getElementById('restartBtn').addEventListener('click',()=>{document.getElementById('gameOver').style.display='none';resetGame();running=true;gameLoop()});
</script>
</body>
</html>`)


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