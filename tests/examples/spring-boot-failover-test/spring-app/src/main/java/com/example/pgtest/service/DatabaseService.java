package com.example.pgtest.service;

import com.example.pgtest.model.TestEntity;
import com.example.pgtest.repository.TestRepository;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import javax.sql.DataSource;
import java.net.InetAddress;
import java.sql.Connection;
import java.sql.SQLException;
import java.time.LocalDateTime;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

@Service
@Slf4j
public class DatabaseService {

    @Autowired
    private TestRepository testRepository;

    @Autowired
    private DataSource dataSource;

    @Transactional(readOnly = true)
    public Map<String, Object> getDatabaseInfo() {
        Map<String, Object> info = new HashMap<>();

        try {
            String version = testRepository.getPostgresVersion();
            String serverAddress = testRepository.getServerAddress();
            Boolean isReplica = testRepository.isInRecovery();

            info.put("version", version);
            info.put("serverAddress", serverAddress);
            info.put("isReplica", isReplica);
            info.put("role", isReplica ? "REPLICA" : "PRIMARY");
            info.put("timestamp", LocalDateTime.now());
            info.put("status", "CONNECTED");

            log.info("Database info: server={}, role={}, version={}",
                    serverAddress, info.get("role"), version);

        } catch (Exception e) {
            log.error("Failed to get database info", e);
            info.put("status", "ERROR");
            info.put("error", e.getMessage());
        }

        return info;
    }

    @Transactional
    public TestEntity writeTestRecord(String message) {
        try {
            String hostname = InetAddress.getLocalHost().getHostName();
            TestEntity entity = new TestEntity(message, hostname);
            TestEntity saved = testRepository.save(entity);

            log.info("Successfully wrote test record: id={}, message={}",
                    saved.getId(), saved.getMessage());

            return saved;
        } catch (Exception e) {
            log.error("Failed to write test record", e);
            throw new RuntimeException("Failed to write test record", e);
        }
    }

    @Transactional(readOnly = true)
    public List<TestEntity> getAllTestRecords() {
        try {
            return testRepository.findAll();
        } catch (Exception e) {
            log.error("Failed to read test records", e);
            throw new RuntimeException("Failed to read test records", e);
        }
    }

    public Map<String, Object> getConnectionPoolInfo() {
        Map<String, Object> poolInfo = new HashMap<>();

        try (Connection conn = dataSource.getConnection()) {
            poolInfo.put("connectionValid", conn.isValid(5));
            poolInfo.put("connectionCatalog", conn.getCatalog());
            poolInfo.put("connectionReadOnly", conn.isReadOnly());
            poolInfo.put("connectionClass", conn.getClass().getName());
            poolInfo.put("timestamp", LocalDateTime.now());

            // Try to get HikariCP specific info
            if (dataSource.getClass().getName().contains("Hikari")) {
                poolInfo.put("poolType", "HikariCP");
            }

        } catch (SQLException e) {
            log.error("Failed to get connection pool info", e);
            poolInfo.put("error", e.getMessage());
        }

        return poolInfo;
    }

    public boolean testConnection() {
        try (Connection conn = dataSource.getConnection()) {
            boolean valid = conn.isValid(5);
            log.info("Connection test: valid={}", valid);
            return valid;
        } catch (SQLException e) {
            log.error("Connection test failed", e);
            return false;
        }
    }
}
