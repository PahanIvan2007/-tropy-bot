const { Router } = require('express');
const { pool } = require('../db');
const { authenticate } = require('../middleware/auth');
const { sendNotification } = require('../telegram');

const router = Router();

router.post('/', authenticate, async (req, res) => {
  const { type, title, message, user_id } = req.body;
  try {
    const targetId = user_id || req.user.id;
    const { rows } = await pool.query(
      `INSERT INTO notifications (user_id, type, title, message) VALUES ($1, $2, $3, $4) RETURNING *`,
      [targetId, type || 'info', title, message]
    );
    sendNotification(targetId, title, message);
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/', authenticate, async (req, res) => {
  const { rows } = await pool.query(
    'SELECT * FROM notifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT 50',
    [req.user.id]
  );
  res.json(rows);
});

router.get('/unread-count', authenticate, async (req, res) => {
  const { rows } = await pool.query(
    'SELECT COUNT(*)::int AS count FROM notifications WHERE user_id = $1 AND is_read = false',
    [req.user.id]
  );
  res.json(rows[0]);
});

router.put('/:id/read', authenticate, async (req, res) => {
  await pool.query(
    'UPDATE notifications SET is_read = true WHERE id = $1 AND user_id = $2',
    [req.params.id, req.user.id]
  );
  res.json({ success: true });
});

router.put('/read-all', authenticate, async (req, res) => {
  await pool.query(
    'UPDATE notifications SET is_read = true WHERE user_id = $1',
    [req.user.id]
  );
  res.json({ success: true });
});

router.post('/register-device', authenticate, async (req, res) => {
  const { token, platform } = req.body;
  if (!token) return res.status(400).json({ error: 'token required' });
  try {
    await pool.query(
      `INSERT INTO device_tokens (user_id, token, platform) VALUES ($1, $2, $3)
       ON CONFLICT (token) DO UPDATE SET user_id = $1, platform = $3, updated_at = NOW()`,
      [req.user.id, token, platform || 'android']
    );
    res.json({ success: true });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
