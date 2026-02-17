const { describe, it, before } = require('node:test');
const assert = require('node:assert');
const request = require('supertest');

// Test when browser returns result without artifacts (triggers || [] branch)
describe('Result without Artifacts', () => {
  let app;

  before(() => {
    process.env.BROWSER_WORKER_URL = 'http://localhost:3001';
    process.env.CONTROL_PLANE_URL = 'http://localhost:8080';
    process.env.ALLOWED_TOOLS = 'browser.navigate';
    delete process.env.TOOL_RUNNER_PORT;
    delete process.env.PORT;

    // Mock fetch returning result WITHOUT artifacts field
    global.fetch = async (url, options) => {
      if (url.includes('/tools/execute') && url.includes('3001')) {
        return {
          ok: true,
          json: async () => ({
            output: { title: 'Test Page' }
            // Note: no artifacts field - should trigger || []
          })
        };
      }
      return { ok: true };
    };

    delete require.cache[require.resolve('./server')];
    const server = require('./server');
    app = server.app;
  });

  it('returns result without artifacts when not provided', async () => {
    const response = await request(app)
      .post('/tools/execute')
      .send({
        run_id: 'run-no-artifacts',
        invocation_id: 'inv-no-artifacts',
        tool_name: 'browser.navigate',
        input: {}
      })
      .expect(200);

    // Response should have output but no artifacts field
    assert.strictEqual(response.body.output.title, 'Test Page');
    assert.strictEqual(response.body.artifacts, undefined);
  });
});
