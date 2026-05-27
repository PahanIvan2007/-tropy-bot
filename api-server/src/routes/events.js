const { Router } = require('express');
const { pool } = require('../db');
const { authenticate, authorize } = require('../middleware/auth');

const router = Router();

router.get('/', async (req, res) => {
  try {
    const { rows } = await pool.query('SELECT * FROM events ORDER BY start_time');
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/', authenticate, authorize('admin', 'point_admin'), async (req, res) => {
  const { title, description, event_type, start_time, end_time, point_id, max_participants, status, visibility } = req.body;
  try {
    const { rows } = await pool.query(
      `INSERT INTO events (title, description, event_type, start_time, end_time, point_id, max_participants, status, visibility, created_by)
       VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING *`,
      [title, description, event_type || 'other', start_time, end_time, point_id, max_participants, status || 'scheduled', visibility || 'public', req.user.id]
    );
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/:id', async (req, res) => {
  try {
    const { rows } = await pool.query('SELECT * FROM events WHERE id = $1', [req.params.id]);
    if (!rows.length) return res.status(404).json({ error: 'Событие не найдено' });
    res.json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
