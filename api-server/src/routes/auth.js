const { Router } = require('express');
const bcrypt = require('bcrypt');
const jwt = require('jsonwebtoken');
const { pool } = require('../db');

const router = Router();
const JWT_SECRET = process.env.JWT_SECRET || 'tropy_kayarana_secret_2026';
const JWT_EXPIRES = '24h';

// POST /api/auth/login
router.post('/login', async (req, res) => {
  const { email, password } = req.body;
  if (!email || !password) {
    return res.status(400).json({ error: 'Email и пароль обязательны' });
  }
  try {
    const { rows } = await pool.query(
      'SELECT id, first_name, last_name, email, role, encrypted_password FROM users WHERE email = $1 AND is_active = true',
      [email]
    );
    if (!rows[0]) {
      return res.status(401).json({ error: 'Неверный email или пароль' });
    }
    const user = rows[0];
    const match = await bcrypt.compare(password, user.encrypted_password);
    if (!match) {
      return res.status(401).json({ error: 'Неверный email или пароль' });
    }
    const token = jwt.sign(
      { id: user.id, email: user.email, role: user.role, name: `${user.first_name} ${user.last_name}` },
      JWT_SECRET,
      { expiresIn: JWT_EXPIRES }
    );
    res.json({
      token,
      user: { id: user.id, email: user.email, role: user.role, name: `${user.first_name} ${user.last_name}` }
    });
  } catch (err) {
    console.error('LOGIN ERROR:', err.message, err.stack?.substring(0, 500));
    res.status(500).json({ error: 'Ошибка сервера' });
  }
});

// POST /api/auth/register
router.post('/register', async (req, res) => {
  const { first_name, last_name, email, password, phone, role } = req.body;
  if (!first_name || !last_name || !email || !password) {
    return res.status(400).json({ error: 'Заполните все обязательные поля' });
  }
  try {
    const existing = await pool.query('SELECT id FROM users WHERE email = $1', [email]);
    if (existing.rows[0]) {
      return res.status(409).json({ error: 'Пользователь с таким email уже существует' });
    }
    const salt = await bcrypt.genSalt(10);
    const hash = await bcrypt.hash(password, salt);
    const { rows } = await pool.query(
      `INSERT INTO users (first_name, last_name, email, phone, role, encrypted_password)
       VALUES ($1, $2, $3, $4, COALESCE($5, 'participant'), $6)
       RETURNING id, first_name, last_name, email, role`,
      [first_name, last_name, email, phone, role || 'participant', hash]
    );
    const user = rows[0];
    const token = jwt.sign(
      { id: user.id, email: user.email, role: user.role, name: `${user.first_name} ${user.last_name}` },
      JWT_SECRET,
      { expiresIn: JWT_EXPIRES }
    );
    res.status(201).json({ token, user });
  } catch (err) {
    res.status(500).json({ error: 'Ошибка сервера' });
  }
});

// GET /api/auth/me
router.get('/me', async (req, res) => {
  const auth = req.headers.authorization;
  if (!auth || !auth.startsWith('Bearer ')) {
    return res.status(401).json({ error: 'Требуется авторизация' });
  }
  try {
    const decoded = jwt.verify(auth.split(' ')[1], JWT_SECRET);
    const { rows } = await pool.query(
      'SELECT id, first_name, last_name, email, role, phone, avatar_url, is_active FROM users WHERE id = $1',
      [decoded.id]
    );
    if (!rows[0]) return res.status(404).json({ error: 'Пользователь не найден' });
    res.json(rows[0]);
  } catch {
    res.status(401).json({ error: 'Недействительный токен' });
  }
});

module.exports = router;
