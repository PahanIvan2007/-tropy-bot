const { Router } = require('express');
const { pool } = require('../db');
const { authenticate, authorize } = require('../middleware/auth');

const router = Router();

// Занятость лодок
router.get('/boat-occupancy', async (req, res) => {
  try {
    const { rows } = await pool.query('SELECT * FROM v_boat_occupancy ORDER BY active_rentals_count DESC');
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// История аренды пользователя
router.get('/user-history/:userId', authenticate, async (req, res) => {
  try {
    const { rows } = await pool.query(
      'SELECT * FROM v_user_rental_history WHERE user_id = $1',
      [req.params.userId]
    );
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Популярность точек
router.get('/point-popularity', async (req, res) => {
  try {
    const { rows } = await pool.query('SELECT * FROM v_point_popularity ORDER BY total_activity DESC');
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Рейтинг игроков
router.get('/player-ratings', async (req, res) => {
  try {
    const { rows } = await pool.query('SELECT * FROM v_player_ratings ORDER BY win_rate_percent DESC');
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/export', authenticate, authorize('admin', 'point_admin'), async (req, res) => {
  try {
    const [boatsRes, routesRes, rentalsRes, usersRes, tournamentsRes] = await Promise.all([
      pool.query('SELECT * FROM boats ORDER BY id'),
      pool.query('SELECT * FROM routes ORDER BY id'),
      pool.query('SELECT * FROM rentals ORDER BY id'),
      pool.query('SELECT id, email, role, first_name, last_name, phone FROM users ORDER BY id'),
      pool.query('SELECT * FROM tournaments ORDER BY id')
    ]);
    res.json({
      boats: boatsRes.rows,
      routes: routesRes.rows,
      rentals: rentalsRes.rows,
      users: usersRes.rows,
      tournaments: tournamentsRes.rows
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
