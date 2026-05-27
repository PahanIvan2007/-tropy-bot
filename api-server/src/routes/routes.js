const { Router } = require('express');
const { pool } = require('../db');
const { authenticate, authorize } = require('../middleware/auth');

const router = Router();

router.get('/', async (req, res) => {
  try {
    const { rows } = await pool.query(
      `SELECT r.*, p.name AS point_name
       FROM routes r
       JOIN points p ON p.id = r.point_id
       ORDER BY r.title`
    );
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/:id', async (req, res) => {
  try {
    const route = await pool.query(
      `SELECT r.*, p.name AS point_name
       FROM routes r
       JOIN points p ON p.id = r.point_id
       WHERE r.id = $1`,
      [req.params.id]
    );
    if (!route.rows.length) return res.status(404).json({ error: 'Маршрут не найден' });

    const points = await pool.query(
      `SELECT rp.*
       FROM route_points rp
       WHERE rp.route_id = $1
       ORDER BY rp.point_order`,
      [req.params.id]
    );

    res.json({ ...route.rows[0], route_points: points.rows });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/', authenticate, authorize('admin', 'point_admin'), async (req, res) => {
  const { title, point_id, description, difficulty, distance_km, estimated_duration, is_inclusive } = req.body;
  try {
    const { rows } = await pool.query(
      `INSERT INTO routes (title, point_id, description, difficulty, distance_km, estimated_duration, is_inclusive)
       VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *`,
      [title, point_id, description, difficulty, distance_km, estimated_duration, is_inclusive || false]
    );
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
