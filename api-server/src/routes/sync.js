const { Router } = require('express');
const { pool } = require('../db');
const { authenticate } = require('../middleware/auth');

const router = Router();

// Офлайн-синхронизация
router.post('/offline', authenticate, async (req, res) => {
  const { events: batch } = req.body;
  if (!Array.isArray(batch) || batch.length === 0) {
    return res.status(400).json({ error: 'Пустой batch событий' });
  }
  try {
    const { rows } = await pool.query(
      `SELECT sync_offline_events($1::jsonb) AS result`,
      [JSON.stringify(batch)]
    );
    res.json(rows[0].result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
