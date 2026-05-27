const { Router } = require('express');
const { pool } = require('../db');

const router = Router();

router.get('/:lat/:lng', async (req, res) => {
  try {
    const { lat, lng } = req.params;
    const resp = await fetch(
      `https://api.open-meteo.com/v1/forecast?latitude=${lat}&longitude=${lng}&current=temperature_2m,precipitation,wind_speed_10m,weather_code&timezone=auto`
    );
    const data = await resp.json();
    res.json(data);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/points', async (req, res) => {
  try {
    const { rows } = await pool.query('SELECT id, name, lat, lng FROM points WHERE status = $1', ['active']);
    const results = await Promise.allSettled(rows.map(async (p) => {
      const resp = await fetch(
        `https://api.open-meteo.com/v1/forecast?latitude=${p.lat}&longitude=${p.lng}&current=temperature_2m,precipitation,wind_speed_10m,weather_code&timezone=auto`
      );
      const data = await resp.json();
      return { point_id: p.id, point_name: p.name, weather: data };
    }));
    res.json(results.filter(r => r.status === 'fulfilled').map(r => r.value));
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
