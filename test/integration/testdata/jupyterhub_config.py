# JupyterHub configuration for integration tests

c = get_config()  # noqa

# Basic configuration
c.JupyterHub.bind_url = 'http://0.0.0.0:8000'
c.JupyterHub.hub_bind_url = 'http://0.0.0.0:8081'

# Use DummyAuthenticator for testing
c.JupyterHub.authenticator_class = 'dummy'

# Create a test user
c.Authenticator.allowed_users = {'testuser', 'admin'}
c.Authenticator.admin_users = {'admin'}

# Use SimpleSpawner (no actual spawning needed for OAuth tests)
c.JupyterHub.spawner_class = 'simple'

# Configure services for OAuth testing
c.JupyterHub.services = [
    {
        'name': 'test-service',
        'url': 'http://host.docker.internal:8888',
        'oauth_client_id': 'service-test-service',
        'api_token': 'test-token-12345',
    }
]

# Disable SSL for testing
c.JupyterHub.ssl_cert = ''
c.JupyterHub.ssl_key = ''

# Enable debug logging
c.JupyterHub.log_level = 'DEBUG'
