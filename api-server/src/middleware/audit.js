const { pool } = require('../db');

const audit = (action, entityType) => {
  return async (req, res, next) => {
    const origJson = res.json.bind(res);
    res.json = async function(body) {
      if (res.statusCode < 400) {
        try {
          await pool.query(
            `INSERT INTO audit_logs (user_id, action, entity_type, entity_id, details, ip_address)
             VALUES ($1, $2, $3, $4, $5, $6)`,
            [
              req.user?.id || null,
              action,
              entityType,
              req.params.id || body?.id || null,
              JSON.stringify({ method: req.method, path: req.originalUrl, body: req.method === 'GET' ? null : req.body }),
              req.ip
            ]
          );
        } catch (e) { /* silent */ }
      }
      return origJson(body);
    };
    next();
  };
};

module.exports = { audit };
