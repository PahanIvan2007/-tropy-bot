const jwt = require('jsonwebtoken');
const { pool } = require('../db');

const JWT_SECRET = process.env.JWT_SECRET || 'tropy_kayarana_secret_2026';

const ROLE_HIERARCHY = {
  participant: 1,
  volunteer: 2,
  instructor: 3,
  judge: 3,
  franchisee: 4,
  point_admin: 5,
  admin: 6,
};

function authenticate(req, res, next) {
  const auth = req.headers.authorization;
  if (!auth || !auth.startsWith('Bearer ')) {
    return res.status(401).json({ error: 'Требуется авторизация' });
  }
  try {
    req.user = jwt.verify(auth.split(' ')[1], JWT_SECRET);
    next();
  } catch {
    return res.status(401).json({ error: 'Недействительный токен' });
  }
}

function authorize(...roles) {
  return (req, res, next) => {
    const userRole = req.user?.role;
    if (!userRole) return res.status(403).json({ error: 'Роль не определена' });
    if (!roles.includes(userRole)) {
      if (!roles.some(r => (ROLE_HIERARCHY[userRole] || 0) >= (ROLE_HIERARCHY[r] || 99))) {
        return res.status(403).json({ error: 'Недостаточно прав' });
      }
    }
    next();
  };
}

module.exports = { authenticate, authorize };
