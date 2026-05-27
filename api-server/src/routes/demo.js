const { Router } = require('express');
const { pool } = require('../db');

const router = Router();

// POST /api/demo/boats — добавить лодку (без авторизации)
router.post('/boats', async (req, res) => {
  const { point_id, serial_number, name, boat_type, capacity } = req.body;
  if (!name || !boat_type) return res.status(400).json({ error: 'name и boat_type обязательны' });
  try {
    const { rows } = await pool.query(
      `INSERT INTO boats (point_id, serial_number, name, boat_type, capacity)
       VALUES ($1, $2, $3, $4, $5) RETURNING *`,
      [point_id || 'b0000000-0000-0000-0000-000000000001', serial_number || null, name, boat_type, capacity || 1]
    );
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// POST /api/demo/events — добавить событие
router.post('/events', async (req, res) => {
  const { title, description, event_type, start_time, end_time, point_id } = req.body;
  if (!title || !start_time) return res.status(400).json({ error: 'title и start_time обязательны' });
  try {
    const { rows } = await pool.query(
      `INSERT INTO events (title, description, event_type, start_time, end_time, point_id, created_by)
       VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *`,
      [title, description || null, event_type || 'other', start_time, end_time || start_time,
       point_id || 'b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001']
    );
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// POST /api/demo/routes — добавить маршрут
router.post('/routes', async (req, res) => {
  const { title, difficulty, distance_km, point_id, is_inclusive } = req.body;
  if (!title) return res.status(400).json({ error: 'title обязателен' });
  try {
    const { rows } = await pool.query(
      `INSERT INTO routes (title, difficulty, distance_km, point_id, is_inclusive)
       VALUES ($1, $2, $3, $4, $5) RETURNING *`,
      [title, difficulty || 'easy', distance_km || '1.0',
       point_id || 'b0000000-0000-0000-0000-000000000001', is_inclusive || false]
    );
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
