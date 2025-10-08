package com.example.pgtest.repository;

import com.example.pgtest.model.TestEntity;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import org.springframework.stereotype.Repository;

@Repository
public interface TestRepository extends JpaRepository<TestEntity, Long> {

    @Query(value = "SELECT version()", nativeQuery = true)
    String getPostgresVersion();

    @Query(value = "SELECT cast(inet_server_addr() as text)", nativeQuery = true)
    String getServerAddress();

    @Query(value = "SELECT pg_is_in_recovery()", nativeQuery = true)
    Boolean isInRecovery();
}
