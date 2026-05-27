const { Router } = require('express');
const { pool } = require('../db');
const { authenticate, authorize } = require('../middleware/auth');

const router = Router();

router.get('/tournament/:tournamentId', async (req, res) => {
  try {
    const { rows } = await pool.query(
      `SELECT m.*, t1.name AS team1_name, t2.name AS team2_name
       FROM matches m
       LEFT JOIN teams t1 ON t1.id = m.team1_id
       LEFT JOIN teams t2 ON t2.id = m.team2_id
       WHERE m.tournament_id = $1
       ORDER BY m.round, m.scheduled_time`,
      [req.params.tournamentId]
    );
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.put('/:id/result', authenticate, authorize('judge', 'admin'), async (req, res) => {
  const { score1, score2 } = req.body;
  try {
    const { rows } = await pool.query(
      `UPDATE matches SET score1 = $1, score2 = $2, winner_team_id = CASE WHEN $1 > $2 THEN team1_id WHEN $2 > $1 THEN team2_id ELSE NULL END,
       status = 'finished' WHERE id = $3 RETURNING *`,
      [score1, score2, req.params.id]
    );
    if (!rows.length) return res.status(404).json({ error: 'Матч не найден' });
    res.json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
