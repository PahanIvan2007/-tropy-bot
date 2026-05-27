const { Router } = require('express');
const { pool } = require('../db');
const { authenticate, authorize } = require('../middleware/auth');

const router = Router();

router.get('/', async (req, res) => {
  try {
    const { rows } = await pool.query(
      `SELECT b.*, p.name AS point_name
       FROM boats b
       JOIN points p ON p.id = b.point_id
       ORDER BY b.name`
    );
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/:id', async (req, res) => {
  try {
    const { rows } = await pool.query(
      `SELECT b.*, p.name AS point_name
       FROM boats b
       JOIN points p ON p.id = b.point_id
       WHERE b.id = $1`,
      [req.params.id]
    );
    if (!rows.length) return res.status(404).json({ error: 'Лодка не найдена' });
    res.json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.put('/:id/status', authenticate, authorize('instructor', 'point_admin'), async (req, res) => {
  const { status } = req.body;
  try {
    const { rows } = await pool.query(
      `UPDATE boats SET status = $1, updated_at = NOW() WHERE id = $2 RETURNING *`,
      [status, req.params.id]
    );
    if (!rows.length) return res.status(404).json({ error: 'Лодка не найдена' });
    res.json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/', authenticate, authorize('point_admin', 'admin'), async (req, res) => {
  const { point_id, serial_number, name, boat_type, capacity } = req.body;
  try {
    const { rows } = await pool.query(
      `INSERT INTO boats (point_id, serial_number, name, boat_type, capacity)
       VALUES ($1, $2, $3, $4, $5) RETURNING *`,
      [point_id, serial_number, name, boat_type, capacity]
    );
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/:id/maintenance', authenticate, authorize('instructor', 'point_admin'), async (req, res) => {
  const { reason } = req.body;
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const { rows } = await client.query(
      `UPDATE boats SET status = 'maintenance', updated_at = NOW() WHERE id = $1 RETURNING *`,
      [req.params.id]
    );
    if (!rows.length) {
      await client.query('ROLLBACK');
      return res.status(404).json({ error: 'Лодка не найдена' });
    }
    await client.query(
      `INSERT INTO audit_log (user_id, action, entity_type, entity_id, details)
       VALUES ($1, $2, $3, $4, $5)`,
      [req.user.id, 'maintenance', 'boat', req.params.id, reason]
    );
    await client.query('COMMIT');
    res.json(rows[0]);
  } catch (err) {
    await client.query('ROLLBACK');
    res.status(500).json({ error: err.message });
  } finally {
    client.release();
  }
});

module.exports = router;
