require 'socket'
require 'mysql2'

host = ENV.fetch('DATABASE_HOST', 'database')
user = ENV.fetch('DATABASE_USER', 'developer')
password = File.read(ENV.fetch('DATABASE_PASSWORD_FILE')).strip
Socket.getaddrinfo(host, 3306)
client = Mysql2::Client.new(host: host, username: user, password: password,
                          database: 'development')
client.query('CREATE TABLE IF NOT EXISTS helper_probe (id INT PRIMARY KEY)')
client.query('INSERT IGNORE INTO helper_probe VALUES (1)')
puts "MariaDB: #{client.query('SELECT VERSION() AS version').first.fetch('version')}"
puts "Persisted rows: #{client.query('SELECT COUNT(*) AS count FROM helper_probe').first.fetch('count')}"
