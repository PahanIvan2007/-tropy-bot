const { Router } = require('express');
const multer = require('multer');
const path = require('path');
const { authenticate } = require('../middleware/auth');
const { pool } = require('../db');

const router = Router();

const storage = multer.diskStorage({
  destination: path.join(__dirname, '../../uploads'),
  filename: (req, file, cb) => {
    const ext = path.extname(file.originalname);
    cb(null, `${Date.now()}-${Math.random().toString(36).slice(2, 8)}${ext}`);
  }
});

const upload = multer({
  storage,
  limits: { fileSize: 5 * 1024 * 1024 },
  fileFilter: (req, file, cb) => {
    const allowed = /\.(jpg|jpeg|png|gif|webp|svg)$/i;
    cb(null, allowed.test(path.extname(file.originalname)));
  }
});

router.post('/avatar', authenticate, upload.single('file'), async (req, res) => {
  if (!req.file) return res.status(400).json({ error: 'Файл не загружен' });
  const url = `/uploads/${req.file.filename}`;
  await pool.query('UPDATE users SET avatar_url = $1 WHERE id = $2', [url, req.user.id]);
  res.json({ url });
});

router.post('/boat/:id', authenticate, upload.single('file'), async (req, res) => {
  if (!req.file) return res.status(400).json({ error: 'Файл не загружен' });
  const features = await pool.query('SELECT features FROM boats WHERE id = $1', [req.params.id]);
  if (!features.rows[0]) return res.status(404).json({ error: 'Лодка не найдена' });
  const current = features.rows[0].features || {};
  current.image = `/uploads/${req.file.filename}`;
  await pool.query('UPDATE boats SET features = $1 WHERE id = $2', [JSON.stringify(current), req.params.id]);
  res.json({ url: current.image });
});

router.post('/point/:id', authenticate, upload.single('file'), async (req, res) => {
  if (!req.file) return res.status(400).json({ error: 'Файл не загружен' });
  const url = `/uploads/${req.file.filename}`;
  await pool.query('UPDATE points SET description = COALESCE(description || $1, $1) WHERE id = $2',
    ['\n[image](' + url + ')', req.params.id]);
  res.json({ url });
});

module.exports = router;
