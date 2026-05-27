const { Router } = require('express');
const { pool } = require('../db');
const { authenticate, authorize } = require('../middleware/auth');

const router = Router();

router.get('/', async (req, res) => {
  try {
    const { rows } = await pool.query('SELECT * FROM points ORDER BY name');
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/:id', async (req, res) => {
  try {
    const { rows } = await pool.query('SELECT * FROM points WHERE id = $1', [req.params.id]);
    if (!rows.length) return res.status(404).json({ error: 'Точка не найдена' });
    res.json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/', authenticate, authorize('admin', 'point_admin'), async (req, res) => {
  const { name, address, lat, lng, type, phone, working_hours } = req.body;
  try {
    const { rows } = await pool.query(
      `INSERT INTO points (name, address, lat, lng, type, phone, working_hours)
       VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *`,
      [name, address, lat, lng, type || 'rental', phone, working_hours]
    );
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.put('/:id', authenticate, authorize('admin', 'point_admin'), async (req, res) => {
  const { name, address, lat, lng, type, status, phone, working_hours } = req.body;
  try {
    const { rows } = await pool.query(
      `UPDATE points SET name = COALESCE($1, name), address = COALESCE($2, address),
       lat = COALESCE($3, lat), lng = COALESCE($4, lng),
       type = COALESCE($5, type), status = COALESCE($6, status),
       phone = COALESCE($7, phone), working_hours = COALESCE($8, working_hours)
       WHERE id = $9 RETURNING *`,
      [name, address, lat, lng, type, status, phone, working_hours, req.params.id]
    );
    if (!rows.length) return res.status(404).json({ error: 'Точка не найдена' });
    res.json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
