const { Router } = require('express');
const { pool } = require('../db');
const { authenticate } = require('../middleware/auth');

const router = Router();

router.get('/', authenticate, async (req, res) => {
  try {
    const { rows } = await pool.query('SELECT id, email, role, first_name, last_name, phone, created_at FROM users WHERE id = $1', [req.user.id]);
    if (!rows.length) return res.status(404).json({ error: 'Пользователь не найден' });
    res.json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.put('/', authenticate, async (req, res) => {
  const { first_name, last_name, phone } = req.body;
  try {
    const { rows } = await pool.query(
      `UPDATE users SET first_name = COALESCE($1, first_name), last_name = COALESCE($2, last_name),
       phone = COALESCE($3, phone), updated_at = NOW() WHERE id = $4 RETURNING id, email, role, first_name, last_name, phone, created_at`,
      [first_name, last_name, phone, req.user.id]
    );
    if (!rows.length) return res.status(404).json({ error: 'Пользователь не найден' });
    res.json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
