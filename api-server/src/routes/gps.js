const { Router } = require('express');
const { pool } = require('../db');
const { authenticate, authorize } = require('../middleware/auth');

const router = Router();

// Старт GPS-трека
router.post('/start', authenticate, async (req, res) => {
  const { event_id, route_id } = req.body;
  try {
    const { rows } = await pool.query(
      `SELECT start_gps_track($1, $2, $3) AS result`,
      [req.user.id, event_id, route_id]
    );
    res.json(rows[0].result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Добавить GPS-точку
router.post('/point', authenticate, async (req, res) => {
  const { track_id, lat, lng, altitude, speed, heading } = req.body;
  try {
    const { rows } = await pool.query(
      `SELECT add_gps_point($1, $2, $3, $4, $5, $6) AS result`,
      [track_id, lat, lng, altitude, speed, heading]
    );
    res.json(rows[0].result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Завершить трек
router.post('/finish', authenticate, async (req, res) => {
  const { track_id } = req.body;
  try {
    const { rows } = await pool.query(
      `SELECT finish_gps_track($1) AS result`,
      [track_id]
    );
    res.json(rows[0].result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Получить трек
router.get('/tracks/:id', authenticate, async (req, res) => {
  const { rows } = await pool.query(
    `SELECT * FROM gps_tracks WHERE id = $1`,
    [req.params.id]
  );
  if (!rows[0]) return res.status(404).json({ error: 'Трек не найден' });
  res.json(rows[0]);
});

module.exports = router;
