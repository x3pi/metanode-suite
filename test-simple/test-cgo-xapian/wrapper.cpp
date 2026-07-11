#include "wrapper.h"
#include <xapian.h>
#include <iostream>
#include <string>

void CreateSampleDb(const char* dbpath) {
    try {
        Xapian::WritableDatabase db(dbpath, Xapian::DB_CREATE_OR_OVERWRITE);
        for (int i = 1; i <= 100; ++i) {
            Xapian::Document doc;
            doc.add_term("apple");
            doc.set_data("This is document " + std::to_string(i));
            db.add_document(doc);
        }
        db.commit();
        std::cout << "C++: DB created successfully." << std::endl;
    } catch (const Xapian::Error& e) {
        std::cerr << "C++ Error: " << e.get_msg() << std::endl;
    }
}

void WriteDb(const char* dbpath, int start_idx) {
    try {
        Xapian::WritableDatabase db(dbpath, Xapian::DB_OPEN);
        Xapian::Document doc;
        doc.add_term("apple");
        doc.set_data("Updated document " + std::to_string(start_idx));
        db.replace_document(1, doc);
        db.commit();
    } catch (...) {}
}

int SearchDb(const char* dbpath) {
    try {
        Xapian::Database db_local(dbpath);
        
        Xapian::Enquire enquire(db_local);
        Xapian::QueryParser qp;
        qp.set_database(db_local);
        Xapian::Query query = qp.parse_query("apple");
        enquire.set_query(query);
        Xapian::MSet mset = enquire.get_mset(0, 10);
        int count = 0;
        for (Xapian::MSetIterator i = mset.begin(); i != mset.end(); ++i) {
             Xapian::Document doc = i.get_document();
             std::string data = doc.get_data();
             count++;
        }
        return count;
    } catch (const Xapian::Error& e) {
        std::cerr << "C++ Search Error: " << e.get_msg() << std::endl;
        return -1;
    } catch (...) {
        std::cerr << "C++ Unknown Error" << std::endl;
        return -2;
    }
}
