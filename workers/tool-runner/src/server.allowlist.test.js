const { describe, it, before } = require('node:test');
const assert = require('node:assert');
const request = require('supertest');

// Test allowlist with whitespace trimming
describe('Allowlist Whitespace Handling', () => {
  let app;

  before(() => {
    // Set up environment with whitespace in allowlist
    process.env.BROWSER_WORKER_URL = 'http://localhost:3001';
    process.env.CONTROL_PLANE_URL = 'http://localhost:8080';
    process.env.ALLOWED_TOOLS = '  browser.navigate  ,  browser.snapshot  ';
    delete process.env.TOOL_RUNNER_PORT;
    delete process.env.PORT;

    // Mock fetch
    global.fetch = async (url, options) => {
      if (url.includes('/tools/execute') && url.includes('3001')) {
        return {
          ok: true,
          json: async () => ({ output: { title: 'Test' }, artifacts: [] })
        };
      }
      return { ok: true };
    };

    // Require server fresh
    delete require.cache[require.resolve('./server')];
    const server = require('./server');
    app = server.app;
  });

  it('trims whitespace from allowlist entries', async () => {
    const response = await request(app)
      .post('/tools/execute')
      .send({
        run_id: 'run-whitespace',
        invocation_id: 'inv-whitespace',
        tool_name: 'browser.navigate',
        input: { url: 'https://example.com' }
      })
      .expect(200);

    assert.ok(response.body.output);
  });
});
