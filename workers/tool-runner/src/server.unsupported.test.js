const { describe, it, before } = require('node:test');
const assert = require('node:assert');
const request = require('supertest');

// Test unsupported tool error
describe('Unsupported Tool Error', () => {
  let app;

  before(() => {
    // Set up environment with a non-browser tool in allowlist
    process.env.BROWSER_WORKER_URL = 'http://localhost:3001';
    process.env.CONTROL_PLANE_URL = 'http://localhost:8080';
    process.env.ALLOWED_TOOLS = 'browser.navigate,custom.unsupported';
    delete process.env.TOOL_RUNNER_PORT;
    delete process.env.PORT;

    // Mock fetch for control plane events
    global.fetch = async () => ({ ok: true });

    // Require server fresh
    delete require.cache[require.resolve('./server')];
    const server = require('./server');
    app = server.app;
  });

  it('returns 500 for unsupported tool that passes allowlist', async () => {
    const response = await request(app)
      .post('/tools/execute')
      .send({
        run_id: 'run-unsupported',
        invocation_id: 'inv-unsupported',
        tool_name: 'custom.unsupported',
        input: {}
      })
      .expect(500);

    assert.strictEqual(response.body.status, 'failed');
    assert.ok(response.body.error.includes('unsupported tool'));
  });
});
