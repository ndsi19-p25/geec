//
// Created by Michael Xusheng Chen on 11/4/2018.
//

#ifndef LEADER_ELECTION_NODE_H
#define LEADER_ELECTION_NODE_H


#include <stdint.h>
#include <pthread.h>

enum state_t{
    STATE_EMPTY = 0,
    //propose
    STATE_PREPARE_SENT = 1,
    STATE_CONFIRM_SENT = 2,
    STATE_ELECTED = 3,
    STATE_TRANFERRED = 4,
    //follower
    STATE_PREPARED = 5,
    STATE_CONFIRMED = 6
};


struct instance_t{
    enum state_t state;
    uint64_t max_rand;
    uint32_t max_member_idx;
    char addr[20];
    char **prepared_addr;
    int prepared_addr_count;
    char **confirmed_addr;
    int confirmed_addr_count;
    //pthread_spinlock_t lock;
    pthread_mutex_t state_lock;
    pthread_cond_t cond;
    int try_count; 
    uint64_t my_rand; 
};


typedef struct instance_t instance_t;

struct Term_t{
    uint64_t start_block;
    uint64_t len;
    uint64_t cur_block;
    struct sockaddr_in *members;
    uint64_t member_count;
    uint32_t my_idx;
    //Protocol state.
    instance_t *instances; //size is len;'
    char my_account[21]; //trails with '\0'
    int sock;
    pthread_t recvt;

    int should_stop;
    pthread_mutex_t flag_lock;

};

typedef struct Term_t Term_t;



// Term_t* New_Node_fake(int offset, uint64_t start_blk, uint64_t term_len);

Term_t* New_Node(char **ipstrs, int *ports, int offset, char *my_account, int commitee_count, uint64_t start_blk, uint64_t term_len);

int elect(Term_t *term, uint64_t blk, uint64_t *rand, int timeoutMs);

int killGroup(Term_t *term);


char**makeCharArray(int size);

void setArrayString(char **a, char *s, int n);

void freeCharArray(char **a, int size);





#endif //LEADER_ELECTION_NODE_H
