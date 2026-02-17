const { describe, it, before } = require('node:test');
const assert = require('node:assert');

// Test req.body || {} branch with null body
describe('Null Request Body', () => {
  let app;

  before(() => {
    process.env.BROWSER_WORKER_URL = 'http://localhost:3001';
    process.env.CONTROL_PLANE_URL = 'http://localhost:8080';
    process.env.ALLOWED_TOOLS = 'browser.navigate';
    delete process.env.TOOL_RUNNER_PORT;
    delete process.env.PORT;

    delete require.cache[require.resolve('./server')];
    const server = require('./server');
    app = server.app;
  });

  it('handles null req.body by returning 400', async () => {
    // Create a mock request with null body
    const req = {
      body: null
    };
    const res = {
      statusCode: 200,
      status(code) {
        this.statusCode = code;
        return this;
      },
      json(data) {
        this.body = data;
        return this;
      }
    };

    // Find the execute route handler
    const executeRoute = app._router.stack.find(
      layer => layer.route && layer.route.path === '/tools/execute' && layer.route.methods.post
    );

    assert.ok(executeRoute, 'Should find execute route');

    // Call the route handler directly with null body
    const handler = executeRoute.route.stack[0].handle;
    await handler(req, res);

    assert.strictEqual(res.statusCode, 400);
    assert.strictEqual(res.body.error, 'run_id, idempotency_key, and tool_name are required');
  });

  it('handles undefined req.body by returning 400', async () => {
    // Create a mock request with undefined body
    const req = {
      body: undefined
    };
    const res = {
      statusCode: 200,
      status(code) {
        this.statusCode = code;
        return this;
      },
      json(data) {
        this.body = data;
        return this;
      }
    };

    // Find the execute route handler
    const executeRoute = app._router.stack.find(
      layer => layer.route && layer.route.path === '/tools/execute' && layer.route.methods.post
    );

    // Call the route handler directly with undefined body
    const handler = executeRoute.route.stack[0].handle;
    await handler(req, res);

    assert.strictEqual(res.statusCode, 400);
    assert.strictEqual(res.body.error, 'run_id, idempotency_key, and tool_name are required');
  });
});
