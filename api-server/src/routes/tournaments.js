const { Router } = require('express');
const { pool } = require('../db');
const { authenticate, authorize } = require('../middleware/auth');

const router = Router();

// Список турниров
router.get('/', async (req, res) => {
  try {
    const { rows } = await pool.query(
      `SELECT t.*, e.title AS event_title, e.start_time, e.end_time
       FROM tournaments t
       JOIN events e ON e.id = t.event_id
       ORDER BY e.start_time DESC`
    );
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Создать турнир
router.post('/', authenticate, authorize('point_admin', 'admin'), async (req, res) => {
  const { title, format, start_time, end_time, point_id, max_teams, rules } = req.body;
  try {
    const { rows } = await pool.query(
      `SELECT create_tournament($1, $2, $3, $4, $5, $6, $7, $8) AS result`,
      [title, format, start_time, end_time, point_id, req.user.id, max_teams, rules]
    );
    res.json(rows[0].result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Регистрация команды
router.post('/:id/register', authenticate, async (req, res) => {
  const { team_id } = req.body;
  try {
    const { rows } = await pool.query(
      `SELECT register_team_for_tournament($1, $2) AS result`,
      [req.params.id, team_id]
    );
    res.json(rows[0].result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Ввод результата матча
router.put('/matches/:id/result', authenticate, authorize('judge'), async (req, res) => {
  const { score1, score2 } = req.body;
  try {
    const { rows } = await pool.query(
      `SELECT set_match_result($1, $2, $3, $4) AS result`,
      [req.params.id, score1, score2, req.user.id]
    );
    res.json(rows[0].result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Турнирная таблица
router.get('/standings', async (req, res) => {
  try {
    const { rows } = await pool.query(`SELECT * FROM v_tournament_standings`);
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
