#include <xapian.h>
#include <iostream>
#include <thread>
#include <vector>
#include <string>
#include <atomic>
#include <mutex>
#include <chrono>

std::atomic<int> success_count(0);
std::atomic<int> error_count(0);
std::mutex print_mutex;

void log(const std::string& msg) {
    std::lock_guard<std::mutex> lock(print_mutex);
    std::cout << msg << std::endl;
}

// Hàm khởi tạo DB mẫu
void create_sample_db(const std::string& dbpath) {
    Xapian::WritableDatabase db(dbpath, Xapian::DB_CREATE_OR_OVERWRITE);
    
    for (int i = 0; i < 100; ++i) {
        Xapian::Document doc;
        doc.add_term("apple");
        doc.add_term("iphone");
        doc.add_value(0, "Apple iPhone " + std::to_string(i));
        doc.set_data("This is document " + std::to_string(i));
        db.add_document(doc);
    }
    db.commit();
    log("Tạo DB mẫu thành công tại: " + dbpath);
}

// Hàm chạy search trong 1 thread
void search_worker(int thread_id, const std::string& dbpath, int iterations) {
    try {
        // Mỗi thread tự mở hoặc copy DB để test thread-safety của Xapian 
        Xapian::Database db(dbpath);
        
        for (int i = 0; i < iterations; ++i) {
            // Giả lập copy database như cách ta làm trong code Blockchain
            Xapian::Database local_db_copy(db);
            
            Xapian::Enquire enquire(local_db_copy);
            Xapian::Query query("apple");
            enquire.set_query(query);
            
            Xapian::MSet mset = enquire.get_mset(0, 10);
            
            if (mset.size() > 0) {
                success_count++;
            } else {
                error_count++;
            }
        }
    } catch (const Xapian::Error& e) {
        std::lock_guard<std::mutex> lock(print_mutex);
        std::cerr << "Thread " << thread_id << " dính lỗi Xapian: " << e.get_msg() << std::endl;
        error_count++;
    } catch (const std::exception& e) {
        std::lock_guard<std::mutex> lock(print_mutex);
        std::cerr << "Thread " << thread_id << " dính lỗi C++: " << e.what() << std::endl;
        error_count++;
    } catch (...) {
        std::lock_guard<std::mutex> lock(print_mutex);
        std::cerr << "Thread " << thread_id << " dính lỗi không xác định (SIGSEGV?)" << std::endl;
        error_count++;
    }
}

int main() {
    std::string dbpath = "./test_xapian_db";
    int num_threads = 10;
    int iterations_per_thread = 5000;
    
    try {
        create_sample_db(dbpath);
        
        log("Bắt đầu chạy " + std::to_string(num_threads) + " luồng, mỗi luồng search " + std::to_string(iterations_per_thread) + " lần...");
        
        auto start = std::chrono::high_resolution_clock::now();
        
        std::vector<std::thread> threads;
        for (int i = 0; i < num_threads; ++i) {
            threads.emplace_back(search_worker, i, dbpath, iterations_per_thread);
        }
        
        for (auto& t : threads) {
            t.join();
        }
        
        auto end = std::chrono::high_resolution_clock::now();
        std::chrono::duration<double, std::milli> elapsed = end - start;
        
        log("Hoàn thành! Thời gian: " + std::to_string(elapsed.count()) + " ms");
        log("Thành công: " + std::to_string(success_count.load()) + " searches");
        log("Lỗi: " + std::to_string(error_count.load()) + " searches");
        
    } catch (const Xapian::Error& e) {
        std::cerr << "Lỗi Xapian ở Main: " << e.get_msg() << std::endl;
        return 1;
    }
    
    return 0;
}
