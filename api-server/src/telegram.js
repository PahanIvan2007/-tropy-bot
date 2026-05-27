const http = require('http');

const GO_BOT_URL = process.env.GO_BOT_URL || 'http://localhost:3001';

let sendNotification = async (userId, title, message) => {
  try {
    const data = JSON.stringify({ user_id: userId, title, message });
    const opts = {
      hostname: 'localhost',
      port: 3001,
      path: '/notify',
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) }
    };
    return new Promise((resolve) => {
      const req = http.request(opts, (res) => {
        res.resume();
        res.on('end', resolve);
      });
      req.on('error', resolve);
      req.write(data);
      req.end();
    });
  } catch {}
};

module.exports = { bot: null, sendNotification };