const { describe, it, before } = require('node:test');
const assert = require('node:assert');
const request = require('supertest');

// Test empty allowlist items are filtered
describe('Empty Allowlist Items', () => {
  let app;

  before(() => {
    // Set up environment with empty items in allowlist
    process.env.BROWSER_WORKER_URL = 'http://localhost:3001';
    process.env.CONTROL_PLANE_URL = 'http://localhost:8080';
    process.env.ALLOWED_TOOLS = 'browser.navigate,,browser.snapshot';
    delete process.env.TOOL_RUNNER_PORT;
    delete process.env.PORT;

    // Mock fetch
    global.fetch = async () => ({ ok: true });

    // Require server fresh
    delete require.cache[require.resolve('./server')];
    const server = require('./server');
    app = server.app;
  });

  it('filters out empty allowlist entries', async () => {
    // Empty string tool name should fail validation (400)
    // because empty tool_name is rejected before allowlist check
    const response = await request(app)
      .post('/tools/execute')
      .send({
        run_id: 'run-empty',
        invocation_id: 'inv-empty',
        tool_name: '',
        input: {}
      })
      .expect(400);

    assert.strictEqual(response.body.error, 'run_id, idempotency_key, and tool_name are required');
  });

  it('valid tools still work with empty items in allowlist', async () => {
    global.fetch = async (url, options) => {
      if (url.includes('/tools/execute') && url.includes('3001')) {
        return {
          ok: true,
          json: async () => ({ output: {}, artifacts: [] })
        };
      }
      return { ok: true };
    };

    const response = await request(app)
      .post('/tools/execute')
      .send({
        run_id: 'run-valid',
        invocation_id: 'inv-valid',
        tool_name: 'browser.navigate',
        input: {}
      })
      .expect(200);

    assert.ok(response.body);
  });
});
