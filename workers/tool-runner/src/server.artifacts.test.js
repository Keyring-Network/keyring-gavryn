const { describe, it, before } = require('node:test');
const assert = require('node:assert');
const request = require('supertest');

// Test result with existing artifacts (no default needed)
describe('Result with Artifacts', () => {
  let app;

  before(() => {
    process.env.BROWSER_WORKER_URL = 'http://localhost:3001';
    process.env.CONTROL_PLANE_URL = 'http://localhost:8080';
    process.env.ALLOWED_TOOLS = 'browser.navigate';
    delete process.env.TOOL_RUNNER_PORT;
    delete process.env.PORT;

    // Mock fetch returning result with existing artifacts
    global.fetch = async (url, options) => {
      if (url.includes('/tools/execute') && url.includes('3001')) {
        return {
          ok: true,
          json: async () => ({
            output: { title: 'Test Page' },
            artifacts: [{ type: 'screenshot', data: 'base64' }]
          })
        };
      }
      return { ok: true };
    };

    delete require.cache[require.resolve('./server')];
    const server = require('./server');
    app = server.app;
  });

  it('uses existing artifacts from browser result', async () => {
    const response = await request(app)
      .post('/tools/execute')
      .send({
        run_id: 'run-artifacts',
        invocation_id: 'inv-artifacts',
        tool_name: 'browser.navigate',
        input: {}
      })
      .expect(200);

    assert.deepStrictEqual(response.body.artifacts, [{ type: 'screenshot', data: 'base64' }]);
  });
});
