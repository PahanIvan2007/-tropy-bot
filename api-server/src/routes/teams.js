const { Router } = require('express');
const { pool } = require('../db');
const { authenticate, authorize } = require('../middleware/auth');

const router = Router();

router.get('/', async (req, res) => {
  try {
    const { rows } = await pool.query(
      `SELECT t.*, u.first_name || ' ' || u.last_name AS captain_name
       FROM teams t
       JOIN users u ON u.id = t.captain_user_id
       ORDER BY t.name`
    );
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/', authenticate, async (req, res) => {
  const { name, description } = req.body;
  try {
    const { rows } = await pool.query(
      `INSERT INTO teams (name, description, captain_user_id) VALUES ($1, $2, $3) RETURNING *`,
      [name, description, req.user.id]
    );
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/:id', async (req, res) => {
  try {
    const team = await pool.query(
      `SELECT t.*, u.first_name || ' ' || u.last_name AS captain_name
       FROM teams t
       JOIN users u ON u.id = t.captain_user_id
       WHERE t.id = $1`,
      [req.params.id]
    );
    if (!team.rows.length) return res.status(404).json({ error: 'Команда не найдена' });

    const members = await pool.query(
      `SELECT tm.*, u.first_name || ' ' || u.last_name AS user_name
       FROM team_members tm
       JOIN users u ON u.id = tm.user_id
       WHERE tm.team_id = $1
       ORDER BY u.last_name`,
      [req.params.id]
    );

    res.json({ ...team.rows[0], members: members.rows });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/:id/members', authenticate, async (req, res) => {
  const { user_id } = req.body;
  try {
    const captain = await pool.query('SELECT captain_user_id FROM teams WHERE id = $1', [req.params.id]);
    if (!captain.rows.length) return res.status(404).json({ error: 'Команда не найдена' });
    if (captain.rows[0].captain_user_id !== req.user.id) {
      return res.status(403).json({ error: 'Только капитан может добавлять участников' });
    }
    const { rows } = await pool.query(
      `INSERT INTO team_members (team_id, user_id) VALUES ($1, $2) RETURNING *`,
      [req.params.id, user_id]
    );
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.delete('/:id/members/:userId', authenticate, async (req, res) => {
  try {
    const captain = await pool.query('SELECT captain_user_id FROM teams WHERE id = $1', [req.params.id]);
    if (!captain.rows.length) return res.status(404).json({ error: 'Команда не найдена' });
    if (captain.rows[0].captain_user_id !== req.user.id) {
      return res.status(403).json({ error: 'Только капитан может удалять участников' });
    }
    const { rowCount } = await pool.query(
      `DELETE FROM team_members WHERE team_id = $1 AND user_id = $2`,
      [req.params.id, req.params.userId]
    );
    if (!rowCount) return res.status(404).json({ error: 'Участник не найден' });
    res.json({ message: 'Участник удалён' });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
