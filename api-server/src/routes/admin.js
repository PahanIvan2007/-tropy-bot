const { Router } = require('express');
const { pool } = require('../db');
const { authenticate, authorize } = require('../middleware/auth');

const router = Router();

router.get('/audit-log', authenticate, authorize('admin'), async (req, res) => {
  try {
    const { limit, offset, action, entity_type } = req.query;
    let sql = `SELECT al.*, u.email AS user_email, u.first_name || ' ' || u.last_name AS user_name
               FROM audit_logs al LEFT JOIN users u ON u.id = al.user_id WHERE 1=1`;
    const params = [];
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

router.get('/users/:id/stats', authenticate, async (req, res) => {
  try {
    const { id } = req.params;
    const rentals = await pool.query('SELECT COUNT(*)::int AS c FROM rentals WHERE user_id = $1', [id]);
    const events = await pool.query('SELECT COUNT(*)::int AS c FROM event_participants WHERE user_id = $1', [id]);
    const teams = await pool.query('SELECT COUNT(*)::int AS c FROM team_members WHERE user_id = $1', [id]);
    res.json({ rentals: rentals.rows[0].c, events: events.rows[0].c, teams: teams.rows[0].c });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/franchises', authenticate, authorize('admin', 'franchisee'), async (req, res) => {
  try {
    const { rows } = await pool.query(
      `SELECT fp.*, p.name AS point_name
       FROM franchise_points fp
       JOIN points p ON p.id = fp.point_id
       ORDER BY p.name`
    );
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/franchises', authenticate, authorize('admin'), async (req, res) => {
  const { franchise_name, owner_user_id, point_id, contract_number, commission_rate } = req.body;
  try {
    const { rows } = await pool.query(
      `INSERT INTO franchise_points (franchise_name, owner_user_id, point_id, contract_number, commission_rate)
       VALUES ($1, $2, $3, $4, $5) RETURNING *`,
      [franchise_name, owner_user_id, point_id, contract_number, commission_rate]
    );
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.put('/franchises/:id', authenticate, authorize('admin'), async (req, res) => {
  const { franchise_name, owner_user_id, point_id, contract_number, commission_rate } = req.body;
  try {
    const { rows } = await pool.query(
      `UPDATE franchise_points
       SET franchise_name = COALESCE($1, franchise_name),
           owner_user_id = COALESCE($2, owner_user_id),
           point_id = COALESCE($3, point_id),
           contract_number = COALESCE($4, contract_number),
           commission_rate = COALESCE($5, commission_rate),
           updated_at = NOW()
       WHERE id = $6 RETURNING *`,
      [franchise_name, owner_user_id, point_id, contract_number, commission_rate, req.params.id]
    );
    if (!rows.length) return res.status(404).json({ error: 'Франшиза не найдена' });
    res.json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/users', authenticate, authorize('admin', 'point_admin'), async (req, res) => {
  try {
    const { rows } = await pool.query(
      'SELECT id, first_name, last_name, email, role, phone FROM users ORDER BY last_name, first_name'
    );
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.get('/inclusive-profiles', authenticate, async (req, res) => {
  try {
    let query = `SELECT ip.*, u.first_name, u.last_name
                 FROM inclusive_profiles ip
                 JOIN users u ON u.id = ip.user_id`;
    const params = [];
    if (req.user.role !== 'admin' && req.user.role !== 'point_admin') {
      query += ` WHERE ip.user_id = $1`;
      params.push(req.user.id);
    }
    query += ` ORDER BY u.last_name, u.first_name`;
    const { rows } = await pool.query(query, params);
    res.json(rows);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/inclusive-profiles', authenticate, async (req, res) => {
  let { user_id, disability_type, needs_escort, allowed_routes, special_equipment, medical_notes, emergency_contact } = req.body;
  if (req.user.role !== 'admin' && req.user.role !== 'point_admin') {
    user_id = req.user.id;
  }
  try {
    const { rows } = await pool.query(
      `INSERT INTO inclusive_profiles (user_id, disability_type, needs_escort, allowed_routes, special_equipment, medical_notes, emergency_contact)
       VALUES ($1, $2, $3, $4, $5, $6, $7)
       ON CONFLICT (user_id) DO UPDATE SET
         disability_type = EXCLUDED.disability_type,
         needs_escort = EXCLUDED.needs_escort,
         allowed_routes = EXCLUDED.allowed_routes,
         special_equipment = EXCLUDED.special_equipment,
         medical_notes = EXCLUDED.medical_notes,
         emergency_contact = EXCLUDED.emergency_contact,
         updated_at = NOW()
       RETURNING *`,
      [user_id, disability_type, needs_escort, allowed_routes, special_equipment, medical_notes, emergency_contact]
    );
    res.status(201).json(rows[0]);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

module.exports = router;
