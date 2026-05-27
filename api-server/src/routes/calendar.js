const { Router } = require('express');
const { pool } = require('../db');
const { authenticate } = require('../middleware/auth');

const router = Router();

router.get('/', authenticate, async (req, res) => {
  try {
    const [rentalsRes, boatsRes] = await Promise.all([
      pool.query(
        `SELECT id, boat_id, user_id, start_time, end_time, status
         FROM rentals
         WHERE status IN ('booked','active','completed')
         ORDER BY start_time`
      ),
      pool.query(`SELECT id, name FROM boats ORDER BY name`)
    ]);
    res.json({ rentals: rentalsRes.rows, boats: boatsRes.rows });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
