const { Router } = require('express');
const { pool } = require('../db');
const { authenticate } = require('../middleware/auth');

const router = Router();

router.get('/', authenticate, async (req, res) => {
  try {
    const { limit, offset, user_id, action, entity_type } = req.query;
    let sql = `SELECT al.*, u.first_name || ' ' || u.last_name AS user_name
               FROM audit_logs al LEFT JOIN users u ON al.user_id = u.id WHERE 1=1`;
    const params = [];
    if (user_id) { params.push(user_id); sql += ` AND al.user_id = $${params.length}`; }
    if (action) { params.push(action); sql += ` AND al.action = $${params.length}`; }
    if (entity_type) { params.push(entity_type); sql += ` AND al.entity_type = $${params.length}`; }
    sql += ' ORDER BY al.created_at DESC LIMIT $' + (params.length + 1) + ' OFFSET $' + (params.length + 2);
    params.push(parseInt(limit) || 100, parseInt(offset) || 0);
    const { rows } = await pool.query(sql, params);
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/stats', authenticate, async (req, res) => {
  try {
    const { rows } = await pool.query(
      `SELECT action, entity_type, COUNT(*)::int AS cnt
       FROM audit_logs GROUP BY action, entity_type ORDER BY cnt DESC LIMIT 20`
    );
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
