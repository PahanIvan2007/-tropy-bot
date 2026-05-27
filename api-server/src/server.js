require('dotenv').config();
const path = require('path');
const express = require('express');
const cors = require('cors');
const { pool } = require('./db');

// Ensure device_tokens table exists
(async () => {
  try {
    await pool.query(`CREATE TABLE IF NOT EXISTS device_tokens (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      token TEXT UNIQUE NOT NULL,
      platform TEXT DEFAULT 'android',
      created_at TIMESTAMPTZ DEFAULT now(),
      updated_at TIMESTAMPTZ DEFAULT now()
    )`);
    await pool.query(`CREATE INDEX IF NOT EXISTS idx_device_tokens_user ON device_tokens (user_id)`);
    console.log('✅ device_tokens table ready');
  } catch (e) { /* table may already exist */ }
})();

const app = express();

// CORS — разрешаем Vercel фронт (без credentials, чтобы fetch() работал)
const allowedOrigins = [
  'http://localhost:4200',
  'https://kayran-ui.vercel.app',
  /\.vercel\.app$/
];
app.use(cors({
  origin: (origin, cb) => {
    if (!origin || allowedOrigins.some(o => o instanceof RegExp ? o.test(origin) : o === origin)) {
      cb(null, true);
    } else {
      cb(new Error('Not allowed by CORS'));
    }
  }
}));
app.use(express.json({ limit: '10mb' }));

// Health check
app.get('/api/health', async (req, res) => {
  try {
    await pool.query('SELECT 1');
    res.json({ status: 'ok', db: 'connected', timestamp: new Date().toISOString() });
  } catch {
    res.status(503).json({ status: 'error', db: 'disconnected' });
  }
});

// Routes
app.use('/api/auth', require('./routes/auth'));
app.use('/api/rentals', require('./routes/rentals'));
app.use('/api/gps', require('./routes/gps'));
app.use('/api/tournaments', require('./routes/tournaments'));
app.use('/api/reports', require('./routes/reports'));
app.use('/api/sync', require('./routes/sync'));
app.use('/api/points', require('./routes/points'));
app.use('/api/boats', require('./routes/boats'));
app.use('/api/routes', require('./routes/routes'));
app.use('/api/teams', require('./routes/teams'));
app.use('/api/events', require('./routes/events'));
app.use('/api/profile', require('./routes/profile'));
app.use('/api/admin', require('./routes/admin'));
app.use('/api/calendar', require('./routes/calendar'));
app.use('/api/matches', require('./routes/matches'));
app.use('/api/notifications', require('./routes/notifications'));
app.use('/api/upload', require('./routes/upload'));
app.use('/api/weather', require('./routes/weather'));
app.use('/api/audit', require('./routes/audit'));
app.use('/api/demo', require('./routes/demo'));
app.use('/game', express.static('public/game'));
app.use('/uploads', express.static('uploads'));

// Telegram bot
const { sendNotification } = require('./telegram');

// Seed notifications for demo users
const seedNotifications = async () => {
  try {
    const count = await pool.query('SELECT COUNT(*)::int AS c FROM notifications');
    if (count.rows[0].c === 0) {
      const users = await pool.query('SELECT id FROM users');
      for (const u of users.rows) {
        await pool.query(
          `INSERT INTO notifications (user_id, type, title, message) VALUES
           ($1, 'welcome', 'Добро пожаловать на Тропы Каярана!', 'Рады видеть вас на платформе. Начните с бронирования лодки.'),
           ($1, 'info', 'Новый маршрут: Озёрная петля', 'Добавлен живописный маршрут средней сложности 5 км.'),
           ($1, 'promo', 'Скидка 20% на аренду SUP', 'Действует до конца недели. Используйте промокод SUP20.')`,
          [u.id]
        );
      }
    }
  } catch (e) { /* silent */ }
};
seedNotifications();

// Слушаем pg_notify для Telegram уведомлений
(async () => {
  try {
    const c = await pool.connect();
    await c.query('LISTEN telegram_notify');
    c.on('notification', async (msg) => {
      try {
        const { user_id, title, message } = JSON.parse(msg.payload);
        await sendNotification(user_id, title, message);
      } catch {}
    });
  } catch {}
})();

// Error handler
app.use((err, req, res, next) => {
  console.error(err.stack);
  res.status(500).json({ error: 'Внутренняя ошибка сервера' });
});

const PORT = process.env.PORT || 3000;
app.listen(PORT, () => {
  console.log(`🌊 Тропы Каярана API запущен на порту ${PORT}`);
});
