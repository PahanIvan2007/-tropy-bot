const { Router } = require('express');
const { pool } = require('../db');
const { authenticate, authorize } = require('../middleware/auth');

const router = Router();

// Бронирование лодки
router.post('/book', authenticate, authorize('participant'), async (req, res) => {
  const { boat_id, point_id, start_time, end_time, notes } = req.body;
  try {
    const { rows } = await pool.query(
      `SELECT book_boat($1, $2, $3, $4, $5, $6) AS result`,
      [req.user.id, boat_id, point_id, start_time, end_time, notes]
    );
    const result = rows[0].result;
    if (result.error) return res.status(400).json(result);
    res.json(result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Выдача лодки по QR
router.post('/scan-pickup', authenticate, authorize('instructor', 'point_admin'), async (req, res) => {
  const { qr_code } = req.body;
  try {
    const { rows } = await pool.query(
      `SELECT scan_qr_pickup($1, $2) AS result`,
      [qr_code, req.user.id]
    );
    const result = rows[0].result;
    if (result.error) return res.status(400).json(result);
    res.json(result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Возврат лодки по QR
router.post('/scan-return', authenticate, authorize('instructor', 'point_admin'), async (req, res) => {
  const { qr_code } = req.body;
  try {
    const { rows } = await pool.query(
      `SELECT scan_qr_return($1, $2) AS result`,
      [qr_code, req.user.id]
    );
    const result = rows[0].result;
    if (result.error) return res.status(400).json(result);
    res.json(result);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Мои аренды
router.get('/my', authenticate, async (req, res) => {
  const { rows } = await pool.query(
    `SELECT r.*, b.name AS boat_name, b.serial_number, p.name AS point_name
     FROM rentals r
     JOIN boats b ON b.id = r.boat_id
     JOIN points p ON p.id = b.point_id
     WHERE r.user_id = $1
     ORDER BY r.created_at DESC`,
    [req.user.id]
  );
  res.json(rows);
});

module.exports = router;
