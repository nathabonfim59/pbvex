export default {
  project: 'example-pbvex',
  defaultTarget: 'local',
  targets: {
    local: { url: 'http://127.0.0.1:8090', metadata: { env: 'local' } },
    staging: { url: 'https://staging.example.com', metadata: { env: 'staging' } },
    production: { url: 'https://example.com', metadata: { env: 'production' } },
  },
};
